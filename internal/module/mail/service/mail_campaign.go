package mailservice

// mail_campaign.go — 群发活动的 CRUD 与投递编排（issue #37）。
//
// ## 为什么投递要「分批展开」而不是「启动时一次性入队」
//
// 一次活动可能有几万收件人。启动接口里一次性写几万条日志 + 入队几万个任务，会让
// HTTP 请求挂住几十秒、内存瞬时膨胀，而且中途失败会留下「一半已入队」的残缺状态。
// 所以 StartCampaign 只做三件事：校验状态、统计人数、改状态并入队一个 dispatch 任务；
// 真正的展开在 worker 里按主键游标一批批做 —— 请求秒回，进度可从计数上看到。
//
// ## 目标人群
//
// 只发给 status=subscribed 的人（未确认同意的 pending 一律不发）。
// 这是合规底线，不是可配置项。

import (
	"context"
	"errors"
	"strings"
	"time"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

// SaveCampaign 新建 / 更新活动（只有草稿可改）。
func (s *Service) SaveCampaign(ctx context.Context, req *maildto.SaveCampaignReq) (res *maildto.CampaignItem, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Subject) == "" {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	if req.AccountID == 0 || req.TemplateID == 0 {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	if req.ID > 0 {
		old, gerr := s.m.GetCampaign(ctx, req.ID)
		if gerr != nil {
			return nil, errors.New(mailenums.ErrCampaignNotFound)
		}
		if old.Status != mailmodel.CampaignStatusDraft {
			return nil, errors.New(mailenums.ErrCampaignNotDraft)
		}
		fields := map[string]any{
			"name":        strings.TrimSpace(req.Name),
			"account_id":  req.AccountID,
			"template_id": req.TemplateID,
			"subject":     strings.TrimSpace(req.Subject),
			"update_time": time.Now(),
		}
		if req.TargetTags != nil {
			fields["target_tags"] = mailmodel.StringArray(req.TargetTags)
		}
		if req.Variables != nil {
			fields["variables"] = mailmodel.JSONMap(req.Variables)
		}
		if err = s.m.UpdateCampaignFields(ctx, req.ID, fields); err != nil {
			return nil, err
		}
	} else {
		e := &mailmodel.MailCampaignEntity{
			Name:       strings.TrimSpace(req.Name),
			AccountID:  req.AccountID,
			TemplateID: req.TemplateID,
			Subject:    strings.TrimSpace(req.Subject),
			Status:     mailmodel.CampaignStatusDraft,
			CreateBy:   req.OperatorID,
		}
		if req.TargetTags != nil {
			e.TargetTags = mailmodel.StringArray(req.TargetTags)
		}
		if req.Variables != nil {
			e.Variables = mailmodel.JSONMap(req.Variables)
		}
		if err = s.m.CreateCampaign(ctx, e); err != nil {
			return nil, err
		}
		req.ID = e.ID
	}
	row, err := s.m.GetCampaign(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return campaignItemOf(row), nil
}

// ListCampaigns 活动列表。
func (s *Service) ListCampaigns(ctx context.Context, req *maildto.CampaignListReq) (res *maildto.CampaignListResp, err error) {
	if req == nil {
		req = &maildto.CampaignListReq{}
	}
	page, size := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	list, total, err := s.m.ListCampaigns(ctx, req.Status, (page-1)*size, size)
	if err != nil {
		return nil, err
	}
	res = &maildto.CampaignListResp{Items: make([]maildto.CampaignItem, 0, len(list)), Total: total}
	for _, e := range list {
		res.Items = append(res.Items, *campaignItemOf(e))
	}
	return res, nil
}

// GetCampaign 活动详情。
func (s *Service) GetCampaign(ctx context.Context, id uint64) (res *maildto.CampaignItem, err error) {
	row, err := s.m.GetCampaign(ctx, id)
	if err != nil {
		return nil, errors.New(mailenums.ErrCampaignNotFound)
	}
	return campaignItemOf(row), nil
}

// DeleteCampaign 删除活动（发送中的不允许删）。
func (s *Service) DeleteCampaign(ctx context.Context, id uint64) (err error) {
	row, gerr := s.m.GetCampaign(ctx, id)
	if gerr != nil {
		return errors.New(mailenums.ErrCampaignNotFound)
	}
	if row.Status == mailmodel.CampaignStatusSending {
		return errors.New(mailenums.ErrCampaignSending)
	}
	return s.m.DeleteCampaign(ctx, id)
}

// StartCampaign 启动群发：校验 → 统计人数 → 改状态 → 入队展开任务。
//
// 这里**不展开收件人**（见文件头说明）。目标是「HTTP 秒回 + 进度可观测」。
func (s *Service) StartCampaign(ctx context.Context, req *maildto.StartCampaignReq) (res *maildto.StartCampaignResp, err error) {
	if req == nil || req.CampaignID == 0 {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	c, err := s.m.GetCampaign(ctx, req.CampaignID)
	if err != nil {
		return nil, errors.New(mailenums.ErrCampaignNotFound)
	}
	if c.Status != mailmodel.CampaignStatusDraft && c.Status != mailmodel.CampaignStatusFailed {
		return nil, errors.New(mailenums.ErrCampaignNotDraft)
	}
	// 模板必须先能渲染出来（变量缺失 / 语法错在这里就暴露，而不是展开到一半才发现）。
	tpl, terr := s.m.GetTemplateByID(ctx, c.TemplateID)
	if terr != nil {
		return nil, errors.New(mailenums.ErrTemplateNotFound)
	}
	baseVars := map[string]any{}
	for k, v := range c.Variables {
		baseVars[k] = v
	}
	if _, _, _, rerr := RenderTemplate(tpl.Subject, tpl.BodyHTML, tpl.BodyText, withSampleVars(baseVars)); rerr != nil {
		return nil, rerr
	}

	total, err := s.m.CountSubscribedByTags(ctx, c.TargetTags)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, errors.New(mailenums.ErrCampaignNoRecipient)
	}
	now := time.Now()
	if err = s.m.UpdateCampaignFields(ctx, c.ID, map[string]any{
		"status":       mailmodel.CampaignStatusSending,
		"started_at":   now,
		"total_count":  total,
		"sent_count":   0,
		"failed_count": 0,
		"update_time":  now,
	}); err != nil {
		return nil, err
	}
	if err = enqueueCampaignDispatch(CampaignDispatchPayload{CampaignID: c.ID, AfterID: 0}); err != nil {
		// 队列未启用（开发 / 测试环境常见）：**降级为同步展开**，与 media 模块
		// 「队列未就绪则同步兜底」的做法一致。生产环境应启用队列 —— 同步展开会占住请求，
		// 收件人上万时体验很差，但比「显示发送中却没人干活」强。
		if derr := s.DispatchCampaign(ctx, c.ID, 0); derr != nil {
			// 降级也失败：状态退回草稿，避免留下脏状态。
			_ = s.m.UpdateCampaignFields(ctx, c.ID, map[string]any{"status": mailmodel.CampaignStatusDraft, "update_time": now})
			return nil, derr
		}
	}
	return &maildto.StartCampaignResp{CampaignID: c.ID, Total: total, Queued: true}, nil
}

// withSampleVars 给每个模板变量填一个占位值，用于启动前的「可渲染性」预检。
//
// 只用于校验，不用于发送 —— 预检时把变量缺失挡掉，避免展开到一半才发现。
func withSampleVars(base map[string]any) map[string]any {
	out := map[string]any{}
	for k := range base {
		out[k] = base[k]
	}
	for _, k := range []string{"name", "email", "username"} {
		if _, ok := out[k]; !ok {
			out[k] = ""
		}
	}
	return out
}

func campaignItemOf(e *mailmodel.MailCampaignEntity) *maildto.CampaignItem {
	item := &maildto.CampaignItem{
		ID:          e.ID,
		Name:        e.Name,
		Status:      e.Status,
		AccountID:   e.AccountID,
		TemplateID:  e.TemplateID,
		Subject:     e.Subject,
		TotalCount:  e.TotalCount,
		SentCount:   e.SentCount,
		FailedCount: e.FailedCount,
		TargetTags:  e.TargetTags,
	}
	if item.TargetTags == nil {
		item.TargetTags = []string{}
	}
	if e.StartedAt != nil {
		item.StartedAt = e.StartedAt.Format(time.RFC3339)
	}
	if e.FinishedAt != nil {
		item.FinishedAt = e.FinishedAt.Format(time.RFC3339)
	}
	if e.CreateTime != nil {
		item.CreateTime = e.CreateTime.Format(time.RFC3339)
	}
	return item
}

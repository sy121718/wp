package mailservice

// mail_automation.go — 自动化流程的 CRUD（issue #38 P3）。
//
// 定义的**读写都是整体**：编辑器一次保存整张图，所以没有「改一个节点」这种接口。
// 版本号在每次保存定义时递增 —— 运行中的实例记着自己启动时的版本，
// 改图不会让它们执行到不存在的节点。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
)

// SaveAutomation 新建或更新流程（保存前校验图）。
func (s *Service) SaveAutomation(ctx context.Context, req *maildto.SaveAutomationReq) (res *maildto.AutomationItem, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	if !validTrigger(req.TriggerType) {
		return nil, errors.New(mailenums.ErrAutomationTriggerInvalid)
	}
	var raw map[string]any
	if len(req.Definition) > 0 {
		if uerr := json.Unmarshal(req.Definition, &raw); uerr != nil {
			return nil, errors.New(mailenums.ErrAutomationGraphInvalid + ": " + uerr.Error())
		}
	}
	// 保存时校验：指向不存在的节点 / 有环 / 不可达，都在这里被拒绝（见 graph 文件头说明）。
	def, err := ParseDefinition(raw)
	if err != nil {
		return nil, errors.New(mailenums.ErrAutomationGraphInvalid + ": " + err.Error())
	}
	norm, merr := json.Marshal(def)
	if merr != nil {
		return nil, merr
	}
	var definition mailmodel.JSONMap
	if uerr := json.Unmarshal(norm, &definition); uerr != nil {
		return nil, uerr
	}
	trigger := mailmodel.JSONMap{}
	for k, v := range req.TriggerParams {
		trigger[k] = v
	}

	now := time.Now()
	if req.ID > 0 {
		old, gerr := s.m.GetAutomation(ctx, req.ID)
		if gerr != nil {
			return nil, errors.New(mailenums.ErrAutomationNotFound)
		}
		fields := map[string]any{
			"name":           strings.TrimSpace(req.Name),
			"description":    nullable(req.Description),
			"trigger_type":   req.TriggerType,
			"trigger_params": trigger,
			"definition":     definition,
			// 定义变了就推进版本：运行中的实例继续用它们启动时的版本。
			"version":     old.Version + 1,
			"update_time": now,
		}
		if err = s.m.UpdateAutomationFields(ctx, req.ID, fields); err != nil {
			return nil, err
		}
	} else {
		e := &mailmodel.MailAutomationEntity{
			Name:          strings.TrimSpace(req.Name),
			TriggerType:   req.TriggerType,
			TriggerParams: trigger,
			Definition:    definition,
			Status:        mailmodel.AutomationStatusDraft,
			Version:       1,
			CreateBy:      req.OperatorID,
		}
		if v := strings.TrimSpace(req.Description); v != "" {
			e.Description = &v
		}
		if err = s.m.CreateAutomation(ctx, e); err != nil {
			return nil, err
		}
		req.ID = e.ID
	}
	row, err := s.m.GetAutomation(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return automationItemOf(row), nil
}

// ListAutomations 流程列表。
func (s *Service) ListAutomations(ctx context.Context, req *maildto.AutomationListReq) (res *maildto.AutomationListResp, err error) {
	if req == nil {
		req = &maildto.AutomationListReq{}
	}
	page, size := req.Page, req.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	list, total, err := s.m.ListAutomations(ctx, req.Status, (page-1)*size, size)
	if err != nil {
		return nil, err
	}
	res = &maildto.AutomationListResp{Items: make([]maildto.AutomationItem, 0, len(list)), Total: total}
	for _, e := range list {
		res.Items = append(res.Items, *automationItemOf(e))
	}
	return res, nil
}

// GetAutomation 流程详情。
func (s *Service) GetAutomation(ctx context.Context, id uint64) (res *maildto.AutomationItem, err error) {
	row, err := s.m.GetAutomation(ctx, id)
	if err != nil {
		return nil, errors.New(mailenums.ErrAutomationNotFound)
	}
	return automationItemOf(row), nil
}

// SetAutomationStatus 启用 / 暂停 / 退回草稿。
//
// 暂停**不影响已运行的实例**：它们已经在流程里了，半路停掉会让用户收到一半的流程
// （比如只收到欢迎邮件却没有后续的优惠券）。要阻止新实例进入才是暂停的语义。
func (s *Service) SetAutomationStatus(ctx context.Context, req *maildto.SetAutomationStatusReq) (err error) {
	if req == nil || req.ID == 0 {
		return errors.New(mailenums.ErrInvalidParam)
	}
	switch req.Status {
	case mailmodel.AutomationStatusDraft, mailmodel.AutomationStatusActive, mailmodel.AutomationStatusPaused:
	default:
		return errors.New(mailenums.ErrInvalidParam)
	}
	row, gerr := s.m.GetAutomation(ctx, req.ID)
	if gerr != nil {
		return errors.New(mailenums.ErrAutomationNotFound)
	}
	// 启用前再校验一遍图：草稿期间如果被外部改动过（直接改库），启用就是最后一道关。
	if req.Status == mailmodel.AutomationStatusActive {
		var raw map[string]any
		b, _ := json.Marshal(row.Definition)
		_ = json.Unmarshal(b, &raw)
		if _, verr := ParseDefinition(raw); verr != nil {
			return errors.New(mailenums.ErrAutomationGraphInvalid + ": " + verr.Error())
		}
	}
	return s.m.UpdateAutomationFields(ctx, req.ID, map[string]any{
		"status": req.Status, "update_time": time.Now(),
	})
}

// DeleteAutomation 删除流程（进行中的实例先停掉，避免留下孤儿实例）。
func (s *Service) DeleteAutomation(ctx context.Context, id uint64) (err error) {
	if _, gerr := s.m.GetAutomation(ctx, id); gerr != nil {
		return errors.New(mailenums.ErrAutomationNotFound)
	}
	if err = s.m.StopRunsOfAutomation(ctx, id, time.Now()); err != nil {
		return err
	}
	return s.m.DeleteAutomation(ctx, id)
}

func validTrigger(t string) bool {
	switch t {
	case mailmodel.TriggerManual, mailmodel.TriggerContactCreated, mailmodel.TriggerContactSubscribed,
		mailmodel.TriggerEmailOpened, mailmodel.TriggerEmailClicked, mailmodel.TriggerTagAdded:
		return true
	}
	return false
}

func automationItemOf(e *mailmodel.MailAutomationEntity) *maildto.AutomationItem {
	item := &maildto.AutomationItem{
		ID:            e.ID,
		Name:          e.Name,
		TriggerType:   e.TriggerType,
		TriggerParams: map[string]any{},
		Status:        e.Status,
		Version:       e.Version,
	}
	if e.Description != nil {
		item.Description = *e.Description
	}
	for k, v := range e.TriggerParams {
		item.TriggerParams[k] = v
	}
	def := map[string]any{}
	for k, v := range e.Definition {
		def[k] = v
	}
	if b, err := json.Marshal(def); err == nil {
		item.Definition = b
	}
	if parsed, err := ParseDefinition(e.Definition); err == nil {
		for _, n := range parsed.Nodes {
			item.Nodes = append(item.Nodes, maildto.AutomationNodeItem{
				Key: n.Key, Type: n.Type, Params: n.Params, Next: n.Next, Yes: n.Yes, No: n.No,
			})
		}
	}
	if e.CreateTime != nil {
		item.CreateTime = e.CreateTime.Format(time.RFC3339)
	}
	return item
}

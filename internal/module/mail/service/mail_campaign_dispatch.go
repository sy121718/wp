package mailservice

// mail_campaign_dispatch.go — 群发的分批展开（issue #37）。
//
// 每个任务只处理一批（campaignDispatchBatch 人），处理完若还有剩余就**续排下一段**
// 并带上 lastID 作为游标。这样做的原因：
//
//   · 单个任务有界，不会因为活动有几万人而超时；
//   · 用主键游标而不是 OFFSET —— 展开过程中（可能几分钟）目标名单若发生变化，
//     OFFSET 会漏人或重复，主键游标严格推进；
//   · 中途失败只影响当前这一段，重启任务能从游标处继续（重复投递由日志幂等兜住）。

import (
	"context"
	"strings"
	"time"

	mailmodel "go_wp/internal/module/mail/model"
)

// campaignDispatchBatch 单段处理人数。
const campaignDispatchBatch = 2000

// DispatchCampaign 处理一段群发展开；返回 nil 表示这一段成功（可能还会续排）。
func (s *Service) DispatchCampaign(ctx context.Context, campaignID, afterID uint64) (err error) {
	c, err := s.m.GetCampaign(ctx, campaignID)
	if err != nil {
		// 活动已不存在（被删）：这段任务作废，不再重试。
		return nil
	}
	if c.Status != mailmodel.CampaignStatusSending {
		// 已被取消 / 已完成：停止展开。
		return nil
	}

	tpl, err := s.m.GetTemplateByID(ctx, c.TemplateID)
	if err != nil {
		_ = s.m.UpdateCampaignFields(ctx, campaignID, map[string]any{
			"status": mailmodel.CampaignStatusFailed, "update_time": time.Now(),
		})
		return nil
	}
	baseVars := map[string]any{}
	for k, v := range c.Variables {
		baseVars[k] = v
	}

	batch, err := s.m.ListSubscribedByTags(ctx, c.TargetTags, afterID, campaignDispatchBatch)
	if err != nil {
		return err
	}
	if len(batch) == 0 {
		// 没人了：这一轮展开结束。
		return s.finishCampaign(ctx, campaignID)
	}

	lastID := afterID
	for _, contact := range batch {
		vars := map[string]any{"email": contact.Email}
		for k, v := range baseVars {
			vars[k] = v
		}
		if contact.Name != nil {
			vars["name"] = *contact.Name
		}
		subject, html, text, rerr := RenderTemplate(tpl.Subject, tpl.BodyHTML, tpl.BodyText, vars)
		if rerr != nil {
			// 单个收件人渲染失败（例如他的姓名为空而模板直接用了 .name）：
			// 记一条 failed 日志并继续下一个人，不因一个人中断整场活动。
			_, _ = s.writeCampaignLog(ctx, campaignID, contact.ID, contact.Email, tpl.TemplateKey, tpl.Subject, c.AccountID, mailmodel.LogStatusFailed, rerr.Error())
			continue
		}
		logID, lerr := s.writeCampaignLog(ctx, campaignID, contact.ID, contact.Email, tpl.TemplateKey, subject, c.AccountID, mailmodel.LogStatusPending, "")
		if lerr != nil {
			continue
		}
		_ = enqueueMailSend(MailSendPayload{
			LogID: logID, AccountID: c.AccountID, To: contact.Email,
			Subject: subject, HTML: html, Text: text,
		})
		lastID = contact.ID
	}

	// 这一段取满了，说明后面可能还有人：续排下一段（带游标）。
	if len(batch) == campaignDispatchBatch {
		return enqueueCampaignDispatch(CampaignDispatchPayload{CampaignID: campaignID, AfterID: lastID})
	}
	return s.finishCampaign(ctx, campaignID)
}

// finishCampaign 收尾：标记活动完成。
//
// 注意这里只看「有没有待投递的人」，不把 sent_count 与 total_count 做等值判断 ——
// 投递是异步的，日志还在陆续回写，等计数对齐才收尾可能永远等不到（有人永久失败）。
// 未送达的部分在计数里如实体现，不掩盖。
func (s *Service) finishCampaign(ctx context.Context, campaignID uint64) (err error) {
	return s.m.UpdateCampaignFields(ctx, campaignID, map[string]any{
		"status": mailmodel.CampaignStatusSent, "finished_at": time.Now(), "update_time": time.Now(),
	})
}

// writeCampaignLog 写一条群发日志（带活动与联系人关联）。
func (s *Service) writeCampaignLog(ctx context.Context, campaignID, contactID uint64, to, templateKey, subject string, accountID uint64, status, errMsg string) (id uint64, err error) {
	e := &mailmodel.MailLogEntity{
		ToEmail:     strings.TrimSpace(to),
		Status:      status,
		CampaignID:  &campaignID,
		ContactID:   &contactID,
		TemplateKey: strPtr(strings.TrimSpace(templateKey)),
	}
	if strings.TrimSpace(subject) != "" {
		e.Subject = strPtr(subject)
	}
	if accountID > 0 {
		e.AccountID = &accountID
	}
	if strings.TrimSpace(errMsg) != "" {
		e.ErrorMessage = strPtr(errMsg)
		e.ErrorKind = strPtr("render")
	}
	if err = s.m.CreateLog(ctx, e); err != nil {
		return 0, err
	}
	return e.ID, nil
}

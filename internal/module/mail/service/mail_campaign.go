package mailservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"math"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/database/retention"
	"go_wp/pkg/facing"
	"go_wp/pkg/lifecycle"
	"go_wp/pkg/logger"
	"go_wp/pkg/mailer"
	"go_wp/pkg/queue"
	"go_wp/pkg/utils"
)

func (s *Service) SaveCampaign(ctx context.Context, req *maildto.SaveCampaignReq) (res *maildto.CampaignItem, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Subject) == "" {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	if req.AccountID == 0 || req.TemplateID == 0 {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	if req.ID > 0 {
		old, gerr := s.m.GetCampaign(ctx, req.ID)
		if gerr != nil {
			return nil, mailenums.Err(mailenums.ErrCampaignNotFound)
		}
		if old.Status != mailmodel.CampaignStatusDraft {
			return nil, mailenums.Err(mailenums.ErrCampaignNotDraft)
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

func (s *Service) GetCampaign(ctx context.Context, id uint64) (res *maildto.CampaignItem, err error) {
	row, err := s.m.GetCampaign(ctx, id)
	if err != nil {
		return nil, mailenums.Err(mailenums.ErrCampaignNotFound)
	}
	return campaignItemOf(row), nil
}

func (s *Service) DeleteCampaign(ctx context.Context, id uint64) (err error) {
	_, err = s.DeleteCampaigns(ctx, &maildto.DeleteIDsReq{IDs: []uint64{id}})
	return err
}

// DeleteCampaigns 整批原子删除活动：任一条不存在或正在发送则整批拒绝，
// 逐条原因进 data.items，不把其它 DB 错误改写成 notfound。
func (s *Service) DeleteCampaigns(ctx context.Context, req *maildto.DeleteIDsReq) (deleted int64, err error) {
	if req == nil || len(req.IDs) == 0 {
		return 0, mailenums.Err(mailenums.ErrInvalidParam)
	}
	ids, ok := dedupIDs(req.IDs)
	if !ok || len(ids) == 0 {
		return 0, mailenums.Err(mailenums.ErrInvalidParam)
	}
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		rows, lerr := s.m.CampaignsByIDsTx(ctx, tx, ids)
		if lerr != nil {
			return lerr
		}
		byID := make(map[uint64]*mailmodel.MailCampaignEntity, len(rows))
		for _, row := range rows {
			byID[row.ID] = row
		}
		items := make([]facing.BlockedItem, 0)
		for _, id := range ids {
			row, ok := byID[id]
			switch {
			case !ok:
				items = append(items, blockedIDItem(id, mailenums.ErrCampaignNotFound))
			case row.Status == mailmodel.CampaignStatusSending:
				items = append(items, blockedIDItem(id, mailenums.ErrCampaignSending))
			}
		}
		if len(items) > 0 {
			return rejectBatch(len(ids), items)
		}
		var derr error
		deleted, derr = s.m.DeleteCampaignsByIDsTx(ctx, tx, ids)
		if derr != nil {
			return derr
		}
		if deleted != int64(len(ids)) {
			return rejectBatch(len(ids), blockedIDs(ids, mailenums.ErrCampaignNotFound))
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

func (s *Service) StartCampaign(ctx context.Context, req *maildto.StartCampaignReq) (res *maildto.StartCampaignResp, err error) {
	if req == nil || req.CampaignID == 0 {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	c, err := s.m.GetCampaign(ctx, req.CampaignID)
	if err != nil {
		return nil, mailenums.Err(mailenums.ErrCampaignNotFound)
	}
	if c.Status != mailmodel.CampaignStatusDraft && c.Status != mailmodel.CampaignStatusFailed {
		return nil, mailenums.Err(mailenums.ErrCampaignNotDraft)
	}

	tpl, terr := s.m.GetTemplateByID(ctx, c.TemplateID)
	if terr != nil {
		return nil, mailenums.Err(mailenums.ErrTemplateNotFound)
	}
	baseVars := map[string]any{}
	for k, v := range c.Variables {
		baseVars[k] = v
	}
	campHTML, campText := bodyOf(tpl)
	if _, _, _, rerr := RenderTemplate(tpl.Subject, campHTML, campText, withSampleVars(baseVars)); rerr != nil {
		return nil, rerr
	}

	total, err := s.m.CountSubscribedByTags(ctx, c.TargetTags)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, mailenums.Err(mailenums.ErrCampaignNoRecipient)
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

		if derr := s.DispatchCampaign(ctx, c.ID, 0); derr != nil {

			if rerr := s.m.UpdateCampaignFields(ctx, c.ID, map[string]any{"status": mailmodel.CampaignStatusDraft, "update_time": now}); rerr != nil {
				logger.Scene("mail").With("campaign_id", c.ID).
					Error(rerr, "启动失败后状态退回草稿也失败：活动停在发送中且无人投递")
			}
			return nil, derr
		}
	}
	return &maildto.StartCampaignResp{CampaignID: c.ID, Total: total, Queued: true}, nil
}

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

const campaignDispatchBatch = 2000

func (s *Service) DispatchCampaign(ctx context.Context, campaignID, afterID uint64) (err error) {
	c, err := s.m.GetCampaign(ctx, campaignID)
	if err != nil {

		return nil
	}
	if c.Status != mailmodel.CampaignStatusSending {

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
		sendHTML, sendText := bodyOf(tpl)
		subject, html, text, rerr := RenderTemplate(tpl.Subject, sendHTML, sendText, vars)
		if rerr != nil {

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

	if len(batch) == campaignDispatchBatch {
		return enqueueCampaignDispatch(CampaignDispatchPayload{CampaignID: campaignID, AfterID: lastID})
	}
	return s.finishCampaign(ctx, campaignID)
}

func (s *Service) finishCampaign(ctx context.Context, campaignID uint64) (err error) {
	return s.m.UpdateCampaignFields(ctx, campaignID, map[string]any{
		"status": mailmodel.CampaignStatusSent, "finished_at": time.Now(), "update_time": time.Now(),
	})
}

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

type SendOutcome struct {
	Delivered bool

	Permanent bool

	Kind string

	Message string
}

func (s *Service) sendNow(ctx context.Context, logID, accountID uint64, to, subject, html, text string) error {
	_, outcome, err := s.sendNowOutcome(ctx, logID, accountID, to, subject, html, text)
	if err != nil {
		return err
	}
	if outcome.Delivered || outcome.Permanent || outcome.Kind == string(mailer.KindConfiguration) {
		return nil
	}

	return &temporarySendError{msg: outcome.Message}
}

func (s *Service) sendNowOutcome(ctx context.Context, logID, accountID uint64, to, subject, html, text string) (sender mailer.Sender, outcome SendOutcome, err error) {
	sender, _, serr := s.senderFor(ctx, accountID)
	if serr != nil {

		outcome.Kind = string(mailer.KindConfiguration)
		outcome.Message = serr.Error()
		_ = s.m.UpdateLogResult(ctx, logID, map[string]any{
			"status":        mailmodel.LogStatusFailed,
			"error_kind":    outcome.Kind,
			"error_message": outcome.Message,
		})
		return nil, outcome, nil
	}
	_, sendErr := sender.Send(ctx, &mailer.Message{
		To:      []mailer.Address{{Email: to}},
		Subject: subject,
		HTML:    html,
		Text:    text,
	})
	now := time.Now()
	if sendErr == nil {
		outcome.Delivered = true
		_ = s.m.UpdateLogResult(ctx, logID, map[string]any{
			"status": mailmodel.LogStatusSent, "sent_at": now, "provider": sender.Name(),
		})
		return sender, outcome, nil
	}

	e := mailer.AsError("smtp", sendErr)
	outcome.Kind = string(e.Kind)
	outcome.Message = e.Message
	if outcome.Message == "" && e.Err != nil {
		outcome.Message = e.Err.Error()
	}
	switch e.Kind {
	case mailer.KindTemporary:

		_ = s.m.Transaction(ctx, func(tx *gorm.DB) error {
			if rerr := s.m.IncrLogRetryTx(ctx, tx, logID); rerr != nil {
				return rerr
			}
			return s.m.UpdateLogResultTx(ctx, tx, logID, map[string]any{
				"error_kind": outcome.Kind, "error_message": outcome.Message,
			})
		})
	case mailer.KindPermanent:

		outcome.Permanent = true
		_ = s.m.Transaction(ctx, func(tx *gorm.DB) error {
			if uerr := s.m.UpdateLogResultTx(ctx, tx, logID, map[string]any{
				"status": mailmodel.LogStatusFailed, "error_kind": outcome.Kind, "error_message": outcome.Message,
			}); uerr != nil {
				return uerr
			}
			if aerr := s.m.AddSuppressionTx(ctx, tx, &mailmodel.MailSuppressionEntity{
				Email: to, Reason: mailmodel.SuppressionReasonHardBounce, Source: strPtr("smtp"),
			}); aerr != nil {
				return aerr
			}
			return s.m.UpdateContactStatusByEmailTx(ctx, tx, to, mailmodel.ContactStatusBounced, now)
		})
	default:
		_ = s.m.UpdateLogResult(ctx, logID, map[string]any{
			"status": mailmodel.LogStatusFailed, "error_kind": outcome.Kind, "error_message": outcome.Message,
		})
	}
	return sender, outcome, nil
}

type temporarySendError struct{ msg string }

func (e *temporarySendError) Error() string {
	if e.msg == "" {
		return "临时发送失败，稍后重试"
	}
	return e.msg
}

const TaskMailSend = "mail:send"

type MailSendPayload struct {
	LogID     uint64 `json:"log_id"`
	AccountID uint64 `json:"account_id"`
	To        string `json:"to"`
	Subject   string `json:"subject"`
	HTML      string `json:"html"`
	Text      string `json:"text"`
}

var mailSendTask = queue.NewTask(TaskMailSend, queue.WithQueue("default"), queue.WithMaxRetry(3))

func enqueueMailSend(p MailSendPayload) error {
	if !queue.IsInited() {
		return errors.New("队列未启用，无法异步投递邮件")
	}
	return mailSendTask.Enqueue(p)
}

const TaskMailCampaignDispatch = "mail:campaign_dispatch"

type CampaignDispatchPayload struct {
	CampaignID uint64 `json:"campaign_id"`
	AfterID    uint64 `json:"after_id"`
}

var campaignDispatchTask = queue.NewTask(TaskMailCampaignDispatch, queue.WithQueue("default"), queue.WithMaxRetry(3))

func enqueueCampaignDispatch(p CampaignDispatchPayload) error {
	if !queue.IsInited() {
		return errors.New("队列未启用，无法异步展开群发")
	}
	return campaignDispatchTask.Enqueue(p)
}

func RegisterMailTaskHandler(db *gorm.DB, cipherSecret string) {
	queue.Register(TaskMailSend, handleMailSend(db, cipherSecret))
	queue.Register(TaskMailCampaignDispatch, handleCampaignDispatch(db, cipherSecret))
}

func handleCampaignDispatch(db *gorm.DB, cipherSecret string) queue.Handler {
	return func(ctx context.Context, raw []byte) error {
		var p CampaignDispatchPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.CampaignID == 0 {
			return errors.New("展开任务载荷缺少 campaign_id")
		}
		svc := NewService(mailmodel.NewMailModel(db))
		svc.SetCipherSecret(cipherSecret)
		return svc.DispatchCampaign(ctx, p.CampaignID, p.AfterID)
	}
}

func handleMailSend(db *gorm.DB, cipherSecret string) queue.Handler {
	return func(ctx context.Context, raw []byte) error {
		var p MailSendPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.LogID == 0 || strings.TrimSpace(p.To) == "" {
			return errors.New("投递任务载荷缺少 log_id 或收件人")
		}

		svc := NewService(mailmodel.NewMailModel(db))
		svc.SetCipherSecret(cipherSecret)

		logRow, err := svc.m.GetLog(ctx, p.LogID)
		if err != nil {

			return nil
		}
		if logRow.Status != mailmodel.LogStatusPending {
			return nil
		}

		sender, _, serr := svc.senderFor(ctx, p.AccountID)
		if serr != nil {

			_ = svc.m.UpdateLogResult(ctx, p.LogID, map[string]any{
				"status":        mailmodel.LogStatusFailed,
				"error_kind":    string(mailer.KindConfiguration),
				"error_message": serr.Error(),
			})
			return nil
		}

		_, sendErr := sender.Send(ctx, &mailer.Message{
			To:      []mailer.Address{{Email: p.To}},
			Subject: p.Subject,
			HTML:    p.HTML,
			Text:    p.Text,
		})
		now := time.Now()
		if sendErr == nil {

			_ = svc.m.Transaction(ctx, func(tx *gorm.DB) error {
				if uerr := svc.m.UpdateLogResultTx(ctx, tx, p.LogID, map[string]any{
					"status":   mailmodel.LogStatusSent,
					"sent_at":  now,
					"provider": sender.Name(),
				}); uerr != nil {
					return uerr
				}

				if logRow.CampaignID != nil {
					return svc.m.IncrCampaignCountsTx(ctx, tx, *logRow.CampaignID, 1, 0)
				}
				return nil
			})
			return nil
		}

		e := mailer.AsError("smtp", sendErr)
		kind, msg := string(e.Kind), e.Message
		if msg == "" && e.Err != nil {
			msg = e.Err.Error()
		}
		switch e.Kind {
		case mailer.KindTemporary:

			_ = svc.m.Transaction(ctx, func(tx *gorm.DB) error {
				if rerr := svc.m.IncrLogRetryTx(ctx, tx, p.LogID); rerr != nil {
					return rerr
				}
				return svc.m.UpdateLogResultTx(ctx, tx, p.LogID, map[string]any{
					"error_kind":    kind,
					"error_message": msg,
				})
			})
			return sendErr
		case mailer.KindPermanent:

			_ = svc.m.Transaction(ctx, func(tx *gorm.DB) error {
				if uerr := svc.m.UpdateLogResultTx(ctx, tx, p.LogID, map[string]any{
					"status":        mailmodel.LogStatusFailed,
					"error_kind":    kind,
					"error_message": msg,
				}); uerr != nil {
					return uerr
				}
				if logRow.CampaignID != nil {
					if cerr := svc.m.IncrCampaignCountsTx(ctx, tx, *logRow.CampaignID, 0, 1); cerr != nil {
						return cerr
					}
				}
				if aerr := svc.m.AddSuppressionTx(ctx, tx, &mailmodel.MailSuppressionEntity{
					Email: p.To, Reason: mailmodel.SuppressionReasonHardBounce, Source: strPtr("smtp"),
				}); aerr != nil {
					return aerr
				}
				return svc.m.UpdateContactStatusByEmailTx(ctx, tx, p.To, mailmodel.ContactStatusBounced, now)
			})
			return nil
		default:
			_ = svc.m.UpdateLogResult(ctx, p.LogID, map[string]any{
				"status":        mailmodel.LogStatusFailed,
				"error_kind":    kind,
				"error_message": msg,
			})
			return nil
		}
	}
}

const (
	mailEventRetainDays = 180

	mailRetentionBatch = 500

	mailRetentionInterval = 24 * time.Hour

	mailTotalsSweepLimit = 500
)

func (s *Service) EnsureCampaignTotals(ctx context.Context, cutoff time.Time) (fixed int, err error) {
	if s == nil || s.m == nil {
		return 0, nil
	}
	ids, lerr := s.m.ListCampaignsWithExpiredEvents(ctx, cutoff, mailTotalsSweepLimit)
	if lerr != nil {
		return 0, lerr
	}
	for _, id := range ids {
		counts, cerr := s.m.CountEventsByType(ctx, id)
		if cerr != nil {
			logger.Scene("mail").With("campaign_id", id).Error(cerr, "统计活动事件失败，跳过固化")
			continue
		}

		opened := counts[mailmodel.EventTypeOpen]
		clicked := counts[mailmodel.EventTypeClick]
		n, uerr := s.m.SetCampaignEventTotalsIfUnset(ctx, id, opened, clicked)
		if uerr != nil {
			logger.Scene("mail").With("campaign_id", id).Error(uerr, "固化活动事件汇总失败")
			continue
		}
		if n > 0 {
			fixed++
		}
	}
	return fixed, nil
}

func (s *Service) PurgeRetention(ctx context.Context) (deleted int64, err error) {
	if s == nil || s.m == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	retain := mailEventRetainDays * 24 * time.Hour
	cutoff := now.Add(-retain)
	fixed, ferr := s.EnsureCampaignTotals(ctx, cutoff)
	if ferr != nil {
		logger.Scene("mail").Error(ferr, "固化活动事件汇总失败")
	} else if fixed > 0 {
		logger.Scene("mail").With("fixed", fixed).Info("已固化活动事件汇总")
	}
	tasks := []retention.Task{
		{
			Name: "mail_campaign_events", Table: "mail_campaign_events", TimeColumn: "create_time",
			Retain: retain, BatchSize: mailRetentionBatch,
			Note: "一次打开/点击一行；报表用「固化汇总 + 保留期内明细」，因此明细可过期清理",
			Sweep: func(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
				return s.m.DeleteEventsBefore(ctx, cutoff, limit)
			},
		},
		{
			Name: "mail_logs", Table: "mail_logs", TimeColumn: "create_time",
			Retain: retain, BatchSize: mailRetentionBatch,
			Note: "逐封发送留档；可追溯价值随时间衰减，异常排查窗口远小于保留期",
			Sweep: func(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
				return s.m.DeleteLogsBefore(ctx, cutoff, limit)
			},
		},
		{
			Name: "mail_automation_node_logs", Table: "mail_automation_node_logs", TimeColumn: "create_time",
			Retain: retain, BatchSize: mailRetentionBatch,
			Note: "自动化流程逐节点一行（启用后增长最快）；按「最近发生了什么」的排障视图定位而非常年留档",
			Sweep: func(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
				return s.m.DeleteNodeLogsBefore(ctx, cutoff, limit)
			},
		},
	}
	outcomes := retention.RunAll(ctx, tasks, now)
	total, failed := retention.Summary(outcomes)
	if len(failed) > 0 {
		logger.Scene("mail").With("failed", failed).Warn("邮件保留期任务部分失败")
	}
	if total > 0 {
		logger.Scene("mail").With("deleted", total).Info("已清理过期邮件明细")
	}
	return total, nil
}

// StartMailRetentionScheduler 启动邮件明细保留期清理；返回的 stop 取消循环并等待在途一轮结束。
//
// ctx 是进程/装配级上下文：收尾时先 cancel、再 stop 等待，最后关数据库。
func StartMailRetentionScheduler(ctx context.Context, svc *Service) func() {
	if utils.IsTestProcess() {
		return func() {}
	}
	return startMailRetentionScheduler(ctx, svc, mailRetentionInterval)
}

// StartMailRetentionSchedulerWithInterval 与上同，只是显式指定间隔（测试与运维用）。
func StartMailRetentionSchedulerWithInterval(ctx context.Context, svc *Service, interval time.Duration) func() {
	return startMailRetentionScheduler(ctx, svc, interval)
}

func startMailRetentionScheduler(ctx context.Context, svc *Service, interval time.Duration) func() {
	if svc == nil {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if interval <= 0 {
		interval = mailRetentionInterval
	}
	loopCtx, cancelLoop := context.WithCancel(ctx)
	wait := lifecycle.PeriodicTask{
		Interval: interval,
		Timeout:  10 * time.Minute,
		Round: func(runCtx context.Context) {
			if _, err := svc.PurgeRetention(runCtx); err != nil {
				logger.Scene("mail").Error(err, "邮件明细保留期清理失败（下一轮重试）")
			}
		},
		OnPanic: func(r any) {
			logger.Scene("mail").Error(fmt.Errorf("panic: %v", r),
				"邮件明细保留期清理单轮 panic（已收敛，下一轮照常；不影响进程）")
		},
	}.Start(loopCtx)

	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			cancelLoop()
			wait()
		})
	}
}

func (s *Service) CampaignReport(ctx context.Context, campaignID uint64, page, pageSize int) (res *maildto.CampaignReport, err error) {
	if campaignID == 0 {
		return nil, mailenums.Err(mailenums.ErrInvalidParam)
	}
	c, err := s.m.GetCampaign(ctx, campaignID)
	if err != nil {
		return nil, mailenums.Err(mailenums.ErrCampaignNotFound)
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}

	res = &maildto.CampaignReport{
		Campaign: *campaignItemOf(c),
		Sent:     c.SentCount,
		Failed:   c.FailedCount,
		Target:   c.TotalCount,
	}

	types := []struct {
		eventType string
		dst       *int64
	}{
		{mailmodel.EventTypeOpen, &res.Opened},
		{mailmodel.EventTypeClick, &res.Clicked},
		{mailmodel.EventTypeUnsubscribe, &res.Unsubscribed},
		{mailmodel.EventTypeComplaint, &res.Complained},
		{mailmodel.EventTypeBounce, &res.Bounced},
	}
	for _, t := range types {
		if n, cerr := s.m.CountDistinctContactsByEvent(ctx, campaignID, t.eventType); cerr == nil {
			*t.dst = n
		}
	}
	counts, _ := s.m.CountEventsByType(ctx, campaignID)
	res.OpenEvents = counts[mailmodel.EventTypeOpen]
	res.ClickEvents = counts[mailmodel.EventTypeClick]

	delivered := res.Target - res.Failed
	if delivered > 0 {
		res.OpenRate = round1(float64(res.Opened) / float64(delivered) * 100)
		res.ClickRate = round1(float64(res.Clicked) / float64(delivered) * 100)
	}

	links, lerr := s.m.ClickRanking(ctx, campaignID, 20)
	if lerr == nil {
		res.Links = make([]maildto.LinkStat, 0, len(links))
		for _, l := range links {
			res.Links = append(res.Links, maildto.LinkStat{URL: l.URL, Total: l.Total, Contacts: l.Contacts})
		}
	}

	rows, total, rerr := s.m.ListRecipients(ctx, campaignID, (page-1)*pageSize, pageSize)
	if rerr == nil {
		res.Total = total
		res.Recipients = make([]maildto.RecipientItem, 0, len(rows))
		for _, r := range rows {
			item := maildto.RecipientItem{
				ContactID: r.ContactID,
				Email:     r.Email,
				Status:    r.Status,
				Opened:    r.Opened,
				Clicked:   r.Clicked,
			}
			if r.Name != nil {
				item.Name = *r.Name
			}
			if r.SentAt != nil {
				item.SentAt = r.SentAt.Format(time.RFC3339)
			}
			if r.ErrorKind != nil {
				item.ErrorKind = *r.ErrorKind
			}
			item.StatusTone = recipientStatusTone(item.ErrorKind)
			res.Recipients = append(res.Recipients, item)
		}
	}
	return res, nil
}

func recipientStatusTone(errorKind string) string {
	if errorKind != "" {
		return "warn"
	}
	return "mute"
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

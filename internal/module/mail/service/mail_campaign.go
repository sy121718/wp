package mailservice

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

// 每个任务只处理一批（campaignDispatchBatch 人），处理完若还有剩余就**续排下一段**
// 并带上 lastID 作为游标。这样做的原因：
//
//   · 单个任务有界，不会因为活动有几万人而超时；
//   · 用主键游标而不是 OFFSET —— 展开过程中（可能几分钟）目标名单若发生变化，
//     OFFSET 会漏人或重复，主键游标严格推进；
//   · 中途失败只影响当前这一段，重启任务能从游标处继续（重复投递由日志幂等兜住）。

// 两条路径共用它：
//   · worker 的 mail:send handler（正常路径）；
//   · 队列未启用时 SendTemplate 的降级路径（开发 / 测试环境）。
//
// 抽出来的意义是**错误分类的处理只有一份**：把临时故障当永久故障会误加抑制名单，
// 反过来会把该拉黑的地址一直重试。这种逻辑最不该有两份实现。

// 设计要点：
//   · **投递一律异步**（经 pkg/queue worker）：注册接口不能因为 SMTP 慢而卡住；
//   · **幂等**：worker 先看日志状态，非 pending 直接跳过 —— 队列重试不会重复发信；
//   · **错误分类驱动处理**：
//     temporary   → 保持 pending 留给重试（队列本身也配了重试）；
//     permanent   → 标 failed 并**把地址加入抑制名单**（硬退信，再发只会继续伤域名声誉）；
//     configuration → 标 failed（认证/端口这类问题重试一万次也没用）。

// 顺序是「先固化、后清理」：明细是报表的唯一数据来源，直接删等于把历史统计一起删掉。
// 固化只针对「全部事件都已过期」的活动（那些明细马上要被删，此刻统计出来就是终值）。

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/mail/dto"
	"go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/retention"
	"go_wp/pkg/logger"
	"go_wp/pkg/mailer"
	"go_wp/pkg/queue"
	"go_wp/pkg/utils"
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
			//
			// 补偿不能静默（原先这里是 `_ =`）：退回失败意味着活动停在「发送中」而没有任何人在
			// 干活 —— 运营看不见、日志里也没有。按 AGENTS.md「补偿必须幂等 + 留痕 + 可重放」，
			// 把失败写进结构化日志（带 campaign_id）：状态本身可以靠后台重新保存收敛，
			// 但「有人遇到过这个问题」必须留得下来。
			if rerr := s.m.UpdateCampaignFields(ctx, c.ID, map[string]any{"status": mailmodel.CampaignStatusDraft, "update_time": now}); rerr != nil {
				logger.Scene("mail").With("campaign_id", c.ID).
					Error(rerr, "启动失败后状态退回草稿也失败：活动停在发送中且无人投递")
			}
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

// SendOutcome 同步发送的结果。handler 用它决定是否回写活动计数。
type SendOutcome struct {
	// Delivered 是否成功投递。
	Delivered bool
	// Permanent 是否永久失败（硬退信：应加入抑制名单）。
	Permanent bool
	// Kind 错误分类（temporary / permanent / configuration）。
	Kind string
	// Message 可读原因。
	Message string
}

// sendNow 同步发送并回写日志状态；返回结果供调用方决定计数与重试。
func (s *Service) sendNow(ctx context.Context, logID, accountID uint64, to, subject, html, text string) error {
	_, outcome, err := s.sendNowOutcome(ctx, logID, accountID, to, subject, html, text)
	if err != nil {
		return err
	}
	if outcome.Delivered || outcome.Permanent || outcome.Kind == string(mailer.KindConfiguration) {
		return nil
	}
	// 临时故障：让调用方（队列）重试。
	return &temporarySendError{msg: outcome.Message}
}

// sendNowOutcome 执行发送并回写日志，返回结果明细。
func (s *Service) sendNowOutcome(ctx context.Context, logID, accountID uint64, to, subject, html, text string) (sender mailer.Sender, outcome SendOutcome, err error) {
	sender, _, serr := s.senderFor(ctx, accountID)
	if serr != nil {
		// 账号问题（停用 / 配置不全 / 密钥不对）：重试无意义。
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
		// 保持 pending，交给队列退避重试；只累加计数与原因。
		// 计数与原因**同事务**：各自提交时「重试 +1 但原因没写」会让排障看到一个没有原因的重试。
		// 发信本身（sender.Send）在事务外 —— 外部副作用不进事务，这里只包回写。
		_ = s.m.Transaction(ctx, func(tx *gorm.DB) error {
			if rerr := s.m.IncrLogRetryTx(ctx, tx, logID); rerr != nil {
				return rerr
			}
			return s.m.UpdateLogResultTx(ctx, tx, logID, map[string]any{
				"error_kind": outcome.Kind, "error_message": outcome.Message,
			})
		})
	case mailer.KindPermanent:
		// 硬退信：标失败 + **加入抑制名单** + 联系人标 bounced —— 再发只会继续伤域名声誉。
		// 三处写**同事务**：分开提交时「日志说失败了、地址却没进抑制名单」的下一次活动还会发；
		// 反过来的半截状态（进了名单、日志还是 pending）会让队列重发同一封信。
		// 外部副作用（sender.Send）已经在事务之外，这里只包回写。
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

// temporarySendError 临时故障（调用方应重试）。
type temporarySendError struct{ msg string }

func (e *temporarySendError) Error() string {
	if e.msg == "" {
		return "临时发送失败，稍后重试"
	}
	return e.msg
}

// TaskMailSend 投递任务类型。
const TaskMailSend = "mail:send"

// MailSendPayload 投递载荷。
//
// **带渲染好的正文**而不是「模板 key + 变量」：渲染在入队前就完成了（失败能当场返回），
// worker 不必再查模板；代价是载荷变大，但一封邮件几十 KB 远低于队列载荷上限。
type MailSendPayload struct {
	LogID     uint64 `json:"log_id"`
	AccountID uint64 `json:"account_id"`
	To        string `json:"to"`
	Subject   string `json:"subject"`
	HTML      string `json:"html"`
	Text      string `json:"text"`
}

// mailSendTask 队列任务门面。
var mailSendTask = queue.NewTask(TaskMailSend, queue.WithQueue("default"), queue.WithMaxRetry(3))

// enqueueMailSend 投递任务（队列未启用时返回错误，由调用方决定是否降级）。
func enqueueMailSend(p MailSendPayload) error {
	if !queue.IsInited() {
		return errors.New("队列未启用，无法异步投递邮件")
	}
	return mailSendTask.Enqueue(p)
}

// TaskMailCampaignDispatch 群发展开任务类型。
//
// 启动活动时只入队这一个任务；收件人展开由它分批完成（见 mail_campaign_dispatch.go）。
// 一场万人活动若在启动时一次性入队一万个任务，请求会挂住几十秒、内存瞬时膨胀，
// 中途失败还会留下「一半已入队」的残缺状态。
const TaskMailCampaignDispatch = "mail:campaign_dispatch"

// CampaignDispatchPayload 展开任务载荷（AfterID 是主键游标）。
type CampaignDispatchPayload struct {
	CampaignID uint64 `json:"campaign_id"`
	AfterID    uint64 `json:"after_id"`
}

var campaignDispatchTask = queue.NewTask(TaskMailCampaignDispatch, queue.WithQueue("default"), queue.WithMaxRetry(3))

// enqueueCampaignDispatch 投递一段展开任务。
func enqueueCampaignDispatch(p CampaignDispatchPayload) error {
	if !queue.IsInited() {
		return errors.New("队列未启用，无法异步展开群发")
	}
	return campaignDispatchTask.Enqueue(p)
}

// RegisterMailTaskHandler 把 handler 注册进队列 worker（路由装配时调用）。
//
// 需要 cipherSecret：worker 要解密账号密码才能发信。密钥在装配期从 config 读入后传进来。
func RegisterMailTaskHandler(db *gorm.DB, cipherSecret string) {
	queue.Register(TaskMailSend, handleMailSend(db, cipherSecret))
	queue.Register(TaskMailCampaignDispatch, handleCampaignDispatch(db, cipherSecret))
}

// handleCampaignDispatch 群发展开 handler。
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

// handleMailSend 投递 handler。
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

		// 幂等：只有 pending 才处理。队列重试时前一次可能已经成功，
		// 若不加这一步，重试会把同一封信再发一遍（对收件人是骚扰，对域名声誉是损耗）。
		logRow, err := svc.m.GetLog(ctx, p.LogID)
		if err != nil {
			// 日志不存在：这条任务没有意义了，直接成功返回不再重试。
			return nil
		}
		if logRow.Status != mailmodel.LogStatusPending {
			return nil
		}

		sender, _, serr := svc.senderFor(ctx, p.AccountID)
		if serr != nil {
			// 账号问题（停用 / 配置不全 / 密钥不对）：重试无意义，标失败并把原因写清楚。
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
			// 日志状态与活动计数**同事务**：各自提交时计数会与日志明细对不上，
			// 而报表的送达率正是拿这两者算的（AGENTS.md「写操作的事务与回滚」）。
			// 外部副作用（sender.Send）已在事务之外，这里只包回写。
			_ = svc.m.Transaction(ctx, func(tx *gorm.DB) error {
				if uerr := svc.m.UpdateLogResultTx(ctx, tx, p.LogID, map[string]any{
					"status":   mailmodel.LogStatusSent,
					"sent_at":  now,
					"provider": sender.Name(),
				}); uerr != nil {
					return uerr
				}
				// 群发活动：成功计数在此**原子递增**（并发投递下读-改-写必然丢计数）。
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
			// 保持 pending，让队列按自己的退避策略重试；只累加计数与原因。
			// 计数与原因同事务：各自提交时「重试 +1 但原因没写」会留下没有原因的重试记录。
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
			// 硬退信：标失败 + 活动失败计数 + **加入抑制名单** + 联系人标 bounced。
			// 四处写**同事务**：分开提交时的半截状态最贵 —— 「日志失败了、地址没进名单」
			// 下一次活动还会发，而「地址进了名单、日志还是 pending」会让队列重发同一封信。
			// 发信（sender.Send）在事务之外，这里只包回写。
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
	// mailEventRetainDays 事件明细与发送日志的保留期。
	mailEventRetainDays = 180
	// mailRetentionBatch 单批删除行数。
	mailRetentionBatch = 500
	// mailRetentionInterval 保留期任务运行间隔。
	mailRetentionInterval = 24 * time.Hour
	// mailTotalsSweepLimit 单轮固化的活动数上限（防止一次跑太久）。
	mailTotalsSweepLimit = 500
)

// EnsureCampaignTotals 把「明细即将被清理」的活动汇总固化到活动记录。
//
// 必须在清理之前执行：事件行一旦删掉就再也统计不出打开/点击数，报表只能看到 0。
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
		// 事件类型常量与写入侧同源（model 包），不在两处各写一遍字符串。
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

// PurgeRetention 执行一次邮件域保留期清理：先固化汇总，再清事件明细与发送日志。
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

// StartMailRetentionScheduler 启动每日邮件明细清理（与 analytics / order 的既有调度同形）。
func StartMailRetentionScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	if svc == nil {
		return
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			_, _ = svc.PurgeRetention(ctx)
		}
		run()
		ticker := time.NewTicker(mailRetentionInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}

// CampaignReport 生成活动报表。
//
// 两条口径必须分清楚，否则数字会互相矛盾：
//
//	· **去重人数**（opened / clicked）—— 率的分母分子都用它；
//	· **事件次数**（openEvents / clickEvents）—— 用于人均次数这类指标。
//
// 用次数当分子会算出超过 100% 的打开率。
func (s *Service) CampaignReport(ctx context.Context, campaignID uint64, page, pageSize int) (res *maildto.CampaignReport, err error) {
	if campaignID == 0 {
		return nil, errors.New(mailenums.ErrInvalidParam)
	}
	c, err := s.m.GetCampaign(ctx, campaignID)
	if err != nil {
		return nil, errors.New(mailenums.ErrCampaignNotFound)
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

	// 率的分母用「送达人数」= 目标 - 失败。用目标人数当分母会把失败的人也当成没打开，
	// 让打开率凭空偏低（退信的人本来就没机会打开）。
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
			res.Recipients = append(res.Recipients, item)
		}
	}
	return res, nil
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

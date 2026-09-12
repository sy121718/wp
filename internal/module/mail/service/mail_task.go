package mailservice

// mail_task.go — 邮件投递的 asynq 任务接入（issue #37）。
//
// 设计要点：
//   · **投递一律异步**（经 pkg/queue worker）：注册接口不能因为 SMTP 慢而卡住；
//   · **幂等**：worker 先看日志状态，非 pending 直接跳过 —— 队列重试不会重复发信；
//   · **错误分类驱动处理**：
//     temporary   → 保持 pending 留给重试（队列本身也配了重试）；
//     permanent   → 标 failed 并**把地址加入抑制名单**（硬退信，再发只会继续伤域名声誉）；
//     configuration → 标 failed（认证/端口这类问题重试一万次也没用）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/mailer"
	"go_wp/pkg/queue"
)

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

// RegisterMailTaskHandler 把投递 handler 注册进队列 worker（路由装配时调用）。
//
// 需要 cipherSecret：worker 要解密账号密码才能发信。密钥在装配期从 config 读入后传进来。
func RegisterMailTaskHandler(db *gorm.DB, cipherSecret string) {
	queue.Register(TaskMailSend, handleMailSend(db, cipherSecret))
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
			_ = svc.m.UpdateLogResult(ctx, p.LogID, map[string]any{
				"status":   mailmodel.LogStatusSent,
				"sent_at":  now,
				"provider": sender.Name(),
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
			_ = svc.m.IncrLogRetry(ctx, p.LogID)
			_ = svc.m.UpdateLogResult(ctx, p.LogID, map[string]any{
				"error_kind":    kind,
				"error_message": msg,
			})
			return sendErr
		case mailer.KindPermanent:
			// 硬退信：标失败，并把地址加入抑制名单 —— 再发只会继续伤域名声誉。
			_ = svc.m.UpdateLogResult(ctx, p.LogID, map[string]any{
				"status":        mailmodel.LogStatusFailed,
				"error_kind":    kind,
				"error_message": msg,
			})
			_ = svc.m.AddSuppression(ctx, &mailmodel.MailSuppressionEntity{
				Email: p.To, Reason: mailmodel.SuppressionReasonHardBounce, Source: strPtr("smtp"),
			})
			_ = svc.m.UpdateContactStatusByEmail(ctx, p.To, mailmodel.ContactStatusBounced, now)
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

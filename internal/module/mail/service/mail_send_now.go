package mailservice

// mail_send_now.go — 同步发送一封（issue #38 P3）。
//
// 两条路径共用它：
//   · worker 的 mail:send handler（正常路径）；
//   · 队列未启用时 SendTemplate 的降级路径（开发 / 测试环境）。
//
// 抽出来的意义是**错误分类的处理只有一份**：把临时故障当永久故障会误加抑制名单，
// 反过来会把该拉黑的地址一直重试。这种逻辑最不该有两份实现。

import (
	"context"
	"time"

	"gorm.io/gorm"

	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/pkg/mailer"
)

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

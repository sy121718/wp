// Package mailer — 邮件发送能力（issue #37）。
//
// facade + provider 结构（照 pkg 规范）：本包只做入口与类型别名，实现放在 provider/。
//
// **与 pkg 里其它组件不同的地方（刻意的）**：本包**没有全局单例与 Init()**。
// 发信账号存在 mail_accounts 表、可能多条、可后台随时改 —— 它是**数据**而不是进程级配置，
// 所以由调用方（internal/module/mail）从库里读出来、构造 Sender 使用。
// 这样换 SMTP 不需要重启进程，也不需要 pkg 去认识业务表。
package mailer

import (
	"context"
	"fmt"

	"go_wp/pkg/mailer/provider"
)

// 类型别名：调用方只 import 本包即可。
type (
	// Address 邮件地址。
	Address = provider.Address
	// Attachment 附件。
	Attachment = provider.Attachment
	// Message 一封待发送的邮件。
	Message = provider.Message
	// Result 发送结果。
	Result = provider.Result
	// Sender 发送器抽象。
	Sender = provider.Sender
	// Error 带分类的发送错误。
	Error = provider.Error
	// Kind 错误分类。
	Kind = provider.Kind
	// SMTPConfig SMTP 配置。
	SMTPConfig = provider.SMTPConfig
)

// 错误分类常量。
const (
	KindTemporary     = provider.KindTemporary
	KindPermanent     = provider.KindPermanent
	KindConfiguration = provider.KindConfiguration
)

// 加密方式常量。
const (
	EncryptionNone     = provider.EncryptionNone
	EncryptionSSL      = provider.EncryptionSSL
	EncryptionStartTLS = provider.EncryptionStartTLS
)

// Config 构造发送器所需的最小配置（由调用方从 mail_accounts 的一行映射而来）。
type Config struct {
	Provider   string // smtp（当前唯一实现），将来 ses / mailgun / postmark / sendgrid
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption string
	From       Address
}

// NewSender 按 provider 名构造发送器（配置不合法直接报清楚）。
func NewSender(cfg Config) (Sender, error) {
	switch cfg.Provider {
	case "", "smtp":
		return provider.NewSMTPSender(provider.SMTPConfig{
			Host:       cfg.Host,
			Port:       cfg.Port,
			Username:   cfg.Username,
			Password:   cfg.Password,
			Encryption: cfg.Encryption,
			From:       cfg.From,
		})
	}
	return nil, fmt.Errorf("不支持的邮件发送方式: %s", cfg.Provider)
}

// AsError 把任意错误归类（调用方拿到 err 后判断 Temporary / Permanent 用）。
func AsError(providerName string, err error) *Error { return provider.AsError(providerName, err) }

// Send 便捷发送（构造一次、发一封；批量场景请复用 Sender 实例）。
func Send(ctx context.Context, s Sender, msg *Message) (*Result, error) {
	if s == nil {
		return nil, &Error{Kind: KindConfiguration, Message: "发送器未初始化"}
	}
	return s.Send(ctx, msg)
}

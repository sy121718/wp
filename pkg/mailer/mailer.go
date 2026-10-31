package mailer

import (
	"context"
	"fmt"

	"go_wp/pkg/mailer/provider"
)

type (
	Address = provider.Address

	Attachment = provider.Attachment

	Message = provider.Message

	Result = provider.Result

	Sender = provider.Sender

	Error = provider.Error

	Kind = provider.Kind

	SMTPConfig = provider.SMTPConfig
)

const (
	KindTemporary     = provider.KindTemporary
	KindPermanent     = provider.KindPermanent
	KindConfiguration = provider.KindConfiguration
)

const (
	EncryptionNone     = provider.EncryptionNone
	EncryptionSSL      = provider.EncryptionSSL
	EncryptionStartTLS = provider.EncryptionStartTLS
)

type Config struct {
	Provider   string
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption string
	From       Address
}

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

func AsError(providerName string, err error) *Error { return provider.AsError(providerName, err) }

func Send(ctx context.Context, s Sender, msg *Message) (*Result, error) {
	if s == nil {
		return nil, &Error{Kind: KindConfiguration, Message: "发送器未初始化"}
	}
	return s.Send(ctx, msg)
}

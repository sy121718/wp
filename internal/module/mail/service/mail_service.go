package mailservice

// mail_service.go — 邮箱模块的 Service 本体（issue #37）。

import (
	mailmodel "go_wp/internal/module/mail/model"
)

// Service 邮箱域服务。
//
// 只持有本模块 model；跨模块能力（用户、队列）经契约注入，见各自 Set 方法。
// 持久化唯一入口是 model 的具名方法（AGENTS.md「model 层定位」）。
type Service struct {
	m *mailmodel.MailModel
	// cipherSecret 敏感配置加密密钥（装配期从 config.yaml 的 app.secret 注入）。
	// 为空表示未配置：此时保存发信账号会明确报错，而不是用弱密钥悄悄加密。
	cipherSecret string
}

// NewService 构造。
func NewService(m *mailmodel.MailModel) *Service {
	return &Service{m: m}
}

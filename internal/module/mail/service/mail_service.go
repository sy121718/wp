package mailservice

// mail_service.go — 邮箱模块的 Service 本体（issue #37）。

import (
	mailmodel "go_wp/internal/module/mail/model"
	"net/http"
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
	// webhookClient 回调节点用的 HTTP 客户端（默认 10s 超时 + 不跟随重定向）。
	//
	// 做成可注入字段而不是每次 new：投放侧要能单测「回调 500 / 超时 / 拒绝」这三种失败
	// 形态，而真实网络不可控（测试里发真请求既慢又不确定）。
	webhookClient *http.Client
}

// NewService 构造。
func NewService(m *mailmodel.MailModel) *Service {
	return &Service{m: m}
}

// SetWebhookClient 替换回调节点的 HTTP 客户端（测试与特殊部署用；传 nil 恢复默认）。
//
// 与 SetCipherSecret 同一形态：装配期注入，运行时不变。
func (s *Service) SetWebhookClient(c *http.Client) { s.webhookClient = c }

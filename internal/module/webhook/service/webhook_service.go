// 本文件只放 Service 结构体、构造函数、装配注入与端点密钥加解密；各能力域见
// webhook_endpoint.go（端点管理）、webhook_delivery.go（投递记录与重投）、
// webhook_worker.go（worker 侧投递落定）、webhook_dispatch.go（事件分发），
// 以及既有的 deliver.go / sign.go / ssrf.go / webhook_task.go / webhook_replay*.go。
package webhookservice

import (
	"errors"

	webhookcontract "go_wp/internal/module/webhook/contract"
	webhookmodel "go_wp/internal/module/webhook/model"
	"go_wp/pkg/crypto"
	"net/http"
)

// Service webhook 域服务：只持本模块 model。
type Service struct {
	m *webhookmodel.WebhookModel
	// cipherSecret 端点签名密钥的加密密钥（装配期注入 app.secret）。
	cipherSecret string
	// client 出站 HTTP 客户端（测试可注入假 Transport）。
	client *http.Client
}

// NewService 构造。
func NewService(m *webhookmodel.WebhookModel) *Service {
	return &Service{m: m, client: newWebhookClient()}
}

// SetCipherSecret 注入敏感配置加密密钥。
func (s *Service) SetCipherSecret(secret string) { s.cipherSecret = secret }

// SetHTTPClient 测试注入口（注入受限客户端的替身）。
func (s *Service) SetHTTPClient(c *http.Client) { s.client = c }

// 编译期断言：本 service 实现模块对外契约。
var (
	_ webhookcontract.EndpointService = (*Service)(nil)
	_ webhookcontract.Dispatcher      = (*Service)(nil)
)

// encryptSecret / decryptSecret 密钥加解密（cipherSecret 未配置时明确报错）。
func (s *Service) encryptSecret(plain string) (string, error) {
	if s.cipherSecret == "" {
		return "", errors.New("未配置加密密钥，无法保存 webhook 签名密钥")
	}
	return crypto.Encrypt(plain, s.cipherSecret)
}

func (s *Service) decryptSecret(cipherText string) (string, error) {
	if s.cipherSecret == "" {
		return "", errors.New("未配置加密密钥，无法解密 webhook 签名密钥")
	}
	return crypto.Decrypt(cipherText, s.cipherSecret)
}

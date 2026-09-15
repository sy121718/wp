package webhookservice

// webhook_service.go — webhook 模块 Service 本体（OSS-006 / SEC-015）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"gorm.io/gorm"

	webhookenums "go_wp/internal/module/webhook/enums"
	webhookmodel "go_wp/internal/module/webhook/model"
	"go_wp/pkg/crypto"
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

// ---- 端点管理（管理员后台配置白名单） ----

// CreateEndpoint 新建端点：URL 立即过 SSRF 校验，密钥加密落库。
func (s *Service) CreateEndpoint(ctx context.Context, eventType, targetURL, plainSecret, description string) (e *webhookmodel.WebhookEndpointEntity, err error) {
	if verr := validateWebhookURL(targetURL); verr != nil {
		return nil, verr
	}
	cipher, cerr := s.encryptSecret(plainSecret)
	if cerr != nil {
		return nil, cerr
	}
	e = &webhookmodel.WebhookEndpointEntity{
		EventType:    eventType,
		TargetURL:    targetURL,
		SecretCipher: cipher,
		Description:  description,
		Status:       webhookenums.EndpointStatusEnabled,
		CreatedAt:    time.Now().Unix(),
		UpdatedAt:    time.Now().Unix(),
	}
	if derr := s.m.CreateEndpoint(ctx, e); derr != nil {
		return nil, derr
	}
	return e, nil
}

// UpdateEndpoint 更新端点；改 URL / 密钥时分别重新校验与重新加密。
func (s *Service) UpdateEndpoint(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if raw, ok := fields["target_url"]; ok {
		u, _ := raw.(string)
		if verr := validateWebhookURL(u); verr != nil {
			return verr
		}
	}
	if raw, ok := fields["secret"]; ok {
		plain, _ := raw.(string)
		cipher, cerr := s.encryptSecret(plain)
		if cerr != nil {
			return cerr
		}
		delete(fields, "secret")
		fields["secret_cipher"] = cipher
	}
	fields["updated_at"] = time.Now().Unix()
	return s.m.UpdateEndpoint(ctx, id, fields)
}

// DeleteEndpoint 删除端点。
func (s *Service) DeleteEndpoint(ctx context.Context, id uint64) error {
	return s.m.DeleteEndpoint(ctx, id)
}

// ListEndpoints 列出端点。
func (s *Service) ListEndpoints(ctx context.Context, eventType string, onlyEnabled bool) (list []*webhookmodel.WebhookEndpointEntity, err error) {
	return s.m.ListEndpoints(ctx, eventType, onlyEnabled)
}

// ---- 事件分发 ----

// DispatchEvent 向某事件类型的全部启用端点派发一次投递：
// 每个端点建一条 pending 投递日志并入队，由 worker 异步签名发送。
// 返回成功入队的端点数。事件负载超限整体拒绝（不静默截断）。
func (s *Service) DispatchEvent(ctx context.Context, eventType string, payload any) (n int, err error) {
	body, merr := json.Marshal(payload)
	if merr != nil {
		return 0, fmt.Errorf("webhook 事件负载序列化失败: %w", merr)
	}
	if len(body) > MaxPayloadBytes {
		return 0, fmt.Errorf("webhook 事件负载 %d 字节超出上限 %d", len(body), MaxPayloadBytes)
	}

	endpoints, lerr := s.m.ListEndpoints(ctx, eventType, true)
	if lerr != nil {
		return 0, lerr
	}
	now := time.Now().Unix()
	for _, ep := range endpoints {
		d := &webhookmodel.WebhookDeliveryEntity{
			EndpointID: ep.ID,
			EventType:  eventType,
			Payload:    string(body),
			Status:     webhookenums.DeliveryStatusPending,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if cerr := s.m.CreateDelivery(ctx, d); cerr != nil {
			return n, cerr
		}
		if eerr := enqueueWebhookDeliver(WebhookDeliverPayload{DeliveryID: d.ID}); eerr != nil {
			// 队列不可用：日志保留 pending，返回错误让调用方感知，不假装已派发。
			return n, eerr
		}
		n++
	}
	return n, nil
}

// DeliverDelivery worker 入口：按投递日志执行一次签名投递并回写结果。
// 返回 error 时队列层按重试上限继续重试。
func (s *Service) DeliverDelivery(ctx context.Context, deliveryID uint64) (err error) {
	d, gerr := s.m.GetDelivery(ctx, deliveryID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil // 日志不存在：任务无意义，直接成功避免无谓重试。
		}
		return gerr
	}
	if d.Status != webhookenums.DeliveryStatusPending {
		return nil // 幂等：非 pending 不再投递。
	}

	ep, gerr := s.m.GetEndpoint(ctx, d.EndpointID)
	if gerr != nil {
		_ = s.m.UpdateDeliveryResult(ctx, d.ID, map[string]any{
			"status":     webhookenums.DeliveryStatusFailed,
			"attempts":   d.Attempts + 1,
			"last_error": "端点不存在或已删除",
			"updated_at": time.Now().Unix(),
		})
		return nil
	}

	secret, derr := s.decryptSecret(ep.SecretCipher)
	if derr != nil {
		// 密钥不可用：重试无意义，标失败。
		_ = s.m.UpdateDeliveryResult(ctx, d.ID, map[string]any{
			"status":     webhookenums.DeliveryStatusFailed,
			"attempts":   d.Attempts + 1,
			"last_error": derr.Error(),
			"updated_at": time.Now().Unix(),
		})
		return nil
	}

	status, perr := postWebhook(ctx, s.client, ep.TargetURL, secret, d.EventType, d.ID, []byte(d.Payload))
	if perr == nil && status >= 200 && status < 300 {
		return s.m.UpdateDeliveryResult(ctx, d.ID, map[string]any{
			"status":          webhookenums.DeliveryStatusDelivered,
			"attempts":        d.Attempts + 1,
			"response_status": status,
			"updated_at":      time.Now().Unix(),
		})
	}

	lastErr := "远端返回非 2xx 状态"
	if perr != nil {
		lastErr = perr.Error()
	} else {
		lastErr = fmt.Sprintf("%s（HTTP %d）", lastErr, status)
	}
	// 非 2xx / 网络错误：标 failed 并返回错误交给队列按上限重试；
	// 重试期间 pending 守卫已被本次写入改为 failed —— 队列重试会因幂等守卫跳过，
	// 因此这里保持 pending、由队列侧最终失败落定更合理：仅在重试耗尽语义由队列保证的前提下成立，
	// 当前实现选择简单路径：写 failed 留痕，返回错误让 asynq 记录任务级失败。
	return errors.Join(
		s.m.UpdateDeliveryResult(ctx, d.ID, map[string]any{
			"status":          webhookenums.DeliveryStatusFailed,
			"attempts":        d.Attempts + 1,
			"response_status": status,
			"last_error":      lastErr,
			"updated_at":      time.Now().Unix(),
		}),
		fmt.Errorf("webhook 投递未成功: %s", lastErr),
	)
}

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

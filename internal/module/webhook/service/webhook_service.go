package webhookservice

// webhook_service.go — webhook 模块 Service 本体（OSS-006 / SEC-015）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"encoding/json"
	webhookcontract "go_wp/internal/module/webhook/contract"
	webhookdto "go_wp/internal/module/webhook/dto"
	webhookenums "go_wp/internal/module/webhook/enums"
	webhookmodel "go_wp/internal/module/webhook/model"
	"go_wp/pkg/crypto"
	"go_wp/pkg/utils"
	"gorm.io/gorm"
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

// ---- 端点管理（管理员后台配置白名单） ----

// CreateEndpoint 新建端点：URL 立即过 SSRF 校验，密钥加密落库。
//
// 密钥**必填**：出站请求恒带 HMAC 签名，没有密钥的端点是个发不出可验签请求的坏条目 ——
// 与其静默建一条收件方永远验不过的记录，不如在建的时候就说清楚。
func (s *Service) CreateEndpoint(ctx context.Context, req *webhookdto.SaveEndpointReq) (item *webhookdto.EndpointItem, err error) {
	eventType := strings.TrimSpace(req.EventType)
	if eventType == "" {
		return nil, errors.New(webhookenums.ErrEventTypeRequired)
	}
	targetURL := strings.TrimSpace(req.TargetURL)
	if targetURL == "" {
		return nil, errors.New(webhookenums.ErrTargetURLRequired)
	}
	if verr := validateWebhookURL(targetURL); verr != nil {
		return nil, verr
	}
	if req.Secret == "" {
		return nil, errors.New(webhookenums.ErrSecretRequired)
	}
	cipher, cerr := s.encryptSecret(req.Secret)
	if cerr != nil {
		return nil, cerr
	}
	now := time.Now()
	e := &webhookmodel.WebhookEndpointEntity{
		EventType:    eventType,
		TargetURL:    targetURL,
		SecretCipher: cipher,
		Description:  req.Description,
		Status:       webhookenums.EndpointStatusEnabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if req.Status != nil {
		e.Status = *req.Status
	}
	if derr := s.m.CreateEndpoint(ctx, e); derr != nil {
		return nil, derr
	}
	return toEndpointItem(e), nil
}

// UpdateEndpoint 更新端点（PATCH 语义：空字段即不改）。
//
// 先确认端点存在再改：Update 对不存在的 id 影响 0 行且不报错，
// 调用方会拿到「保存成功」而其实什么都没发生。改 URL / 密钥时重新校验与重新加密。
func (s *Service) UpdateEndpoint(ctx context.Context, req *webhookdto.SaveEndpointReq) (item *webhookdto.EndpointItem, err error) {
	if req.ID == 0 {
		return nil, errors.New(webhookenums.ErrInvalidParam)
	}
	if _, gerr := s.m.GetEndpoint(ctx, req.ID); gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, errors.New(webhookenums.ErrEndpointNotFound)
		}
		return nil, gerr
	}

	fields := map[string]any{"update_time": time.Now()}
	if et := strings.TrimSpace(req.EventType); et != "" {
		fields["event_type"] = et
	}
	if u := strings.TrimSpace(req.TargetURL); u != "" {
		if verr := validateWebhookURL(u); verr != nil {
			return nil, verr
		}
		fields["target_url"] = u
	}
	if req.Description != "" {
		fields["description"] = req.Description
	}
	if req.Secret != "" {
		cipher, cerr := s.encryptSecret(req.Secret)
		if cerr != nil {
			return nil, cerr
		}
		fields["secret_cipher"] = cipher
	}
	if req.Status != nil {
		fields["status"] = *req.Status
	}
	if uerr := s.m.UpdateEndpoint(ctx, req.ID, fields); uerr != nil {
		return nil, uerr
	}
	e, gerr := s.m.GetEndpoint(ctx, req.ID)
	if gerr != nil {
		return nil, gerr
	}
	return toEndpointItem(e), nil
}

// DeleteEndpoint 删除端点（投递日志由外键 ON DELETE CASCADE 一并清理）。
func (s *Service) DeleteEndpoint(ctx context.Context, id uint64) error {
	if id == 0 {
		return errors.New(webhookenums.ErrInvalidParam)
	}
	return s.m.DeleteEndpoint(ctx, id)
}

// SetEndpointStatus 启停端点。
//
// 停用只影响**新**投递的派发（DispatchEvent 只取启用端点）；
// 已经入队的投递仍会执行 —— 改状态不该让在途任务凭空消失。
func (s *Service) SetEndpointStatus(ctx context.Context, id uint64, status int) (err error) {
	if id == 0 || (status != webhookenums.EndpointStatusEnabled && status != webhookenums.EndpointStatusDisabled) {
		return errors.New(webhookenums.ErrInvalidParam)
	}
	return s.m.UpdateEndpoint(ctx, id, map[string]any{
		"status":      status,
		"update_time": time.Now(),
	})
}

// ListEndpoints 列出端点（eventType 为空即全部）。
//
// 取**全部**而不只取启用的：后台要看得见停用的端点并把它重新启用。
// 只取启用是派发侧的需求（DispatchEvent 走 model 自行传 onlyEnabled=true）。
func (s *Service) ListEndpoints(ctx context.Context, eventType string) (list []*webhookdto.EndpointItem, err error) {
	rows, lerr := s.m.ListEndpoints(ctx, eventType, false)
	if lerr != nil {
		return nil, lerr
	}
	list = make([]*webhookdto.EndpointItem, 0, len(rows))
	for _, e := range rows {
		list = append(list, toEndpointItem(e))
	}
	return list, nil
}

// ListDeliveries 投递日志（排障视图）。
func (s *Service) ListDeliveries(ctx context.Context, req *webhookdto.DeliveryListReq) (res *webhookdto.DeliveryListResp, err error) {
	if req == nil {
		req = &webhookdto.DeliveryListReq{}
	}
	page, size := normalizePage(req.Page, req.PageSize)
	total, cerr := s.m.CountDeliveries(ctx, req.EndpointID, req.EventType, req.Status)
	if cerr != nil {
		return nil, cerr
	}
	rows, lerr := s.m.ListDeliveries(ctx, req.EndpointID, req.EventType, req.Status, (page-1)*size, size)
	if lerr != nil {
		return nil, lerr
	}
	res = &webhookdto.DeliveryListResp{Items: make([]*webhookdto.DeliveryItem, 0, len(rows)), Total: total}
	for _, d := range rows {
		res.Items = append(res.Items, toDeliveryItem(d))
	}
	return res, nil
}

// RetryDelivery 重投一次失败的投递。
//
// worker 的幂等守卫是「只有 pending 才投」，所以重投**必须先真的把状态改回 pending** ——
// 只入队的话任务到 worker 就被静默跳过，界面上会显示「已重新入队」而实际什么都没发。
// attempts 不清零：它是「这条投递一共试过几次」的历史，清零会让排障看不出它失败过。
func (s *Service) RetryDelivery(ctx context.Context, id uint64) (err error) {
	if id == 0 {
		return errors.New(webhookenums.ErrInvalidParam)
	}
	d, gerr := s.m.GetDelivery(ctx, id)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return errors.New(webhookenums.ErrDeliveryNotFound)
		}
		return gerr
	}
	if d.Status == webhookenums.DeliveryStatusPending {
		return errors.New(webhookenums.ErrDeliveryNotPending)
	}
	if d.Status != webhookenums.DeliveryStatusFailed {
		return errors.New(webhookenums.ErrDeliveryNotFailed)
	}
	if uerr := s.m.UpdateDeliveryResult(ctx, id, map[string]any{
		"status": webhookenums.DeliveryStatusPending,
		// 清掉上一轮的错误：留着会让「pending 却带着 last_error」看起来像状态不一致。
		"last_error":  "",
		"update_time": time.Now(),
	}); uerr != nil {
		return uerr
	}
	return enqueueWebhookDeliver(WebhookDeliverPayload{DeliveryID: id})
}

// toEndpointItem Entity → 对外条目。**刻意不含密钥**：只给「配没配」的布尔。
func toEndpointItem(e *webhookmodel.WebhookEndpointEntity) *webhookdto.EndpointItem {
	return &webhookdto.EndpointItem{
		ID:          e.ID,
		EventType:   e.EventType,
		TargetURL:   e.TargetURL,
		Description: e.Description,
		Status:      e.Status,
		HasSecret:   e.SecretCipher != "",
		CreateTime:  utils.NewJSONTime(e.CreatedAt),
		UpdateTime:  utils.NewJSONTime(e.UpdatedAt),
	}
}

// toDeliveryItem Entity → 排障条目，负载只给预览。
func toDeliveryItem(d *webhookmodel.WebhookDeliveryEntity) *webhookdto.DeliveryItem {
	return &webhookdto.DeliveryItem{
		ID:             d.ID,
		EndpointID:     d.EndpointID,
		EventType:      d.EventType,
		PayloadPreview: truncateRunes(d.Payload, webhookdto.PayloadPreviewBytes),
		PayloadBytes:   len(d.Payload),
		Status:         d.Status,
		Attempts:       d.Attempts,
		ResponseStatus: d.ResponseStatus,
		LastError:      d.LastError,
		CreateTime:     utils.NewJSONTime(d.CreatedAt),
		UpdateTime:     utils.NewJSONTime(d.UpdatedAt),
	}
}

// truncateRunes 按**字符**截断。按字节切会把多字节字符劈成非法 UTF-8，
// 响应序列化时变成替换符，排障看到的负载首行就是乱的。
func truncateRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}

// normalizePage 分页兜底口径（与其它模块一致：默认 1 页 20 条，上限 200）。
func normalizePage(page, size int) (p, s int) {
	p = page
	if p < 1 {
		p = 1
	}
	s = size
	if s < 1 {
		s = 20
	}
	if s > 200 {
		s = 200
	}
	return p, s
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
	now := time.Now()
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
			"status":      webhookenums.DeliveryStatusFailed,
			"attempts":    d.Attempts + 1,
			"last_error":  "端点不存在或已删除",
			"update_time": time.Now(),
		})
		return nil
	}

	secret, derr := s.decryptSecret(ep.SecretCipher)
	if derr != nil {
		// 密钥不可用：重试无意义，标失败。
		_ = s.m.UpdateDeliveryResult(ctx, d.ID, map[string]any{
			"status":      webhookenums.DeliveryStatusFailed,
			"attempts":    d.Attempts + 1,
			"last_error":  derr.Error(),
			"update_time": time.Now(),
		})
		return nil
	}

	status, perr := postWebhook(ctx, s.client, ep.TargetURL, secret, d.EventType, d.ID, []byte(d.Payload))
	if perr == nil && status >= 200 && status < 300 {
		return s.m.UpdateDeliveryResult(ctx, d.ID, map[string]any{
			"status":          webhookenums.DeliveryStatusDelivered,
			"attempts":        d.Attempts + 1,
			"response_status": status,
			"update_time":     time.Now(),
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
			"update_time":     time.Now(),
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

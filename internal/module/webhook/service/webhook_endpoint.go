package webhookservice

// webhook_endpoint.go — 端点管理：管理员配置的出站目标白名单（URL / 密钥 / 事件类型）。
// 保存与停用都在写库前过一遍 URL 校验与密钥加解密，投递永远只用这里存的 URL。

import (
	"context"
	"errors"
	"strings"
	"time"

	webhookdto "go_wp/internal/module/webhook/dto"
	webhookenums "go_wp/internal/module/webhook/enums"
	webhookmodel "go_wp/internal/module/webhook/model"
	"go_wp/pkg/utils"
	"gorm.io/gorm"
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

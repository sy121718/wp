package webhookservice

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

func (s *Service) CreateEndpoint(ctx context.Context, req *webhookdto.SaveEndpointReq) (item *webhookdto.EndpointItem, err error) {
	eventType := strings.TrimSpace(req.EventType)
	if eventType == "" {
		return nil, webhookenums.Err(webhookenums.ErrEventTypeRequired)
	}
	targetURL := strings.TrimSpace(req.TargetURL)
	if targetURL == "" {
		return nil, webhookenums.Err(webhookenums.ErrTargetURLRequired)
	}
	if verr := validateWebhookURL(targetURL); verr != nil {
		return nil, verr
	}
	if req.Secret == "" {
		return nil, webhookenums.Err(webhookenums.ErrSecretRequired)
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

func (s *Service) UpdateEndpoint(ctx context.Context, req *webhookdto.SaveEndpointReq) (item *webhookdto.EndpointItem, err error) {
	if req.ID == 0 {
		return nil, webhookenums.Err(webhookenums.ErrInvalidParam)
	}
	if _, gerr := s.m.GetEndpoint(ctx, req.ID); gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, webhookenums.Err(webhookenums.ErrEndpointNotFound)
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

func (s *Service) DeleteEndpoint(ctx context.Context, id uint64) error {
	if id == 0 {
		return webhookenums.Err(webhookenums.ErrInvalidParam)
	}
	return s.m.DeleteEndpoint(ctx, id)
}

func (s *Service) SetEndpointStatus(ctx context.Context, id uint64, status int) (err error) {
	if id == 0 || (status != webhookenums.EndpointStatusEnabled && status != webhookenums.EndpointStatusDisabled) {
		return webhookenums.Err(webhookenums.ErrInvalidParam)
	}
	return s.m.UpdateEndpoint(ctx, id, map[string]any{
		"status":      status,
		"update_time": time.Now(),
	})
}

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

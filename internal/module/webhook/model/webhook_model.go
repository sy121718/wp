package webhookmodel

import (
	"context"
	"time"

	"gorm.io/gorm"

	webhookenums "go_wp/internal/module/webhook/enums"
)

type WebhookEndpointEntity struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	EventType    string `gorm:"column:event_type;size:128;not null;index"`
	TargetURL    string `gorm:"column:target_url;size:1024;not null"`
	SecretCipher string `gorm:"column:secret_cipher;size:512;not null"`
	Description  string `gorm:"column:description;size:512"`
	Status       int    `gorm:"column:status;not null;default:1;index"`

	CreatedAt time.Time `gorm:"column:create_time;not null"`
	UpdatedAt time.Time `gorm:"column:update_time;not null"`
}

func (WebhookEndpointEntity) TableName() string { return "webhook_endpoints" }

type WebhookDeliveryEntity struct {
	ID             uint64 `gorm:"primaryKey;autoIncrement"`
	EndpointID     uint64 `gorm:"column:endpoint_id;not null;index"`
	EventType      string `gorm:"column:event_type;size:128;not null;index"`
	Payload        string `gorm:"column:payload;not null"`
	Status         string `gorm:"column:status;size:16;not null;default:'pending';index"`
	Attempts       int    `gorm:"column:attempts;not null;default:0"`
	ResponseStatus int    `gorm:"column:response_status"`
	LastError      string `gorm:"column:last_error;size:1024"`

	CreatedAt time.Time `gorm:"column:create_time;not null"`
	UpdatedAt time.Time `gorm:"column:update_time;not null"`
}

func (WebhookDeliveryEntity) TableName() string { return "webhook_deliveries" }

type WebhookModel struct{ db *gorm.DB }

func NewWebhookModel(db *gorm.DB) *WebhookModel { return &WebhookModel{db: db} }

func (m *WebhookModel) tx(ctx context.Context) *gorm.DB { return m.db.WithContext(ctx) }

func (m *WebhookModel) CreateEndpoint(ctx context.Context, e *WebhookEndpointEntity) error {
	return m.tx(ctx).Create(e).Error
}

func (m *WebhookModel) GetEndpoint(ctx context.Context, id uint64) (e *WebhookEndpointEntity, err error) {
	e = &WebhookEndpointEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *WebhookModel) ListEndpoints(ctx context.Context, eventType string, onlyEnabled bool) (list []*WebhookEndpointEntity, err error) {
	q := m.tx(ctx).Model(&WebhookEndpointEntity{})
	if eventType != "" {
		q = q.Where("event_type = ?", eventType)
	}
	if onlyEnabled {
		q = q.Where("status = ?", 1)
	}
	err = q.Order("id ASC").Find(&list).Error
	return list, err
}

func (m *WebhookModel) UpdateEndpoint(ctx context.Context, id uint64, fields map[string]any) error {
	return m.tx(ctx).Model(&WebhookEndpointEntity{}).Where("id = ?", id).Updates(fields).Error
}

func (m *WebhookModel) DeleteEndpoint(ctx context.Context, id uint64) error {
	return m.tx(ctx).Where("id = ?", id).Delete(&WebhookEndpointEntity{}).Error
}

func (m *WebhookModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *WebhookModel) CreateDelivery(ctx context.Context, e *WebhookDeliveryEntity) error {
	return m.tx(ctx).Create(e).Error
}

func (m *WebhookModel) CreateDeliveriesTx(ctx context.Context, tx *gorm.DB, list []*WebhookDeliveryEntity) error {
	if len(list) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Create(&list).Error
}

func (m *WebhookModel) GetDelivery(ctx context.Context, id uint64) (e *WebhookDeliveryEntity, err error) {
	e = &WebhookDeliveryEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *WebhookModel) UpdateDeliveryResult(ctx context.Context, id uint64, fields map[string]any) error {
	return m.tx(ctx).Model(&WebhookDeliveryEntity{}).Where("id = ?", id).Updates(fields).Error
}

func (m *WebhookModel) ClaimDelivery(ctx context.Context, id uint64, lease time.Duration, at time.Time) (bool, error) {
	at = at.Truncate(time.Microsecond)

	res := m.tx(ctx).Model(&WebhookDeliveryEntity{}).
		Where("id = ? AND (status = ? OR (status = ? AND update_time < ?))",
			id, webhookenums.DeliveryStatusPending,
			webhookenums.DeliveryStatusDelivering, at.Add(-lease)).
		Updates(map[string]any{
			"status":      webhookenums.DeliveryStatusDelivering,
			"update_time": at,
		})
	return res.RowsAffected > 0, res.Error
}

func (m *WebhookModel) ReleaseDeliveryClaim(ctx context.Context, id uint64, at time.Time) error {
	return m.tx(ctx).Model(&WebhookDeliveryEntity{}).
		Where("id = ? AND status = ?", id, webhookenums.DeliveryStatusDelivering).
		Updates(map[string]any{
			"status":      webhookenums.DeliveryStatusPending,
			"update_time": at.Truncate(time.Microsecond),
		}).Error
}

func (m *WebhookModel) MarkDeliveryResult(ctx context.Context, id uint64, status string, responseStatus int, lastErr string, at time.Time) (bool, error) {
	res := m.tx(ctx).Model(&WebhookDeliveryEntity{}).
		Where("id = ? AND status = ?", id, webhookenums.DeliveryStatusDelivering).
		Updates(map[string]any{
			"status":          status,
			"attempts":        gorm.Expr("attempts + 1"),
			"response_status": responseStatus,
			"last_error":      lastErr,
			"update_time":     at.Truncate(time.Microsecond),
		})
	return res.RowsAffected > 0, res.Error
}

func (m *WebhookModel) MarkDeliveryRetryable(ctx context.Context, id uint64, at time.Time) (bool, error) {
	res := m.tx(ctx).Model(&WebhookDeliveryEntity{}).
		Where("id = ? AND status = ?", id, webhookenums.DeliveryStatusFailed).
		Updates(map[string]any{
			"status":      webhookenums.DeliveryStatusPending,
			"last_error":  "",
			"update_time": at.Truncate(time.Microsecond),
		})
	return res.RowsAffected > 0, res.Error
}

func (m *WebhookModel) ListStaleDeliveries(ctx context.Context, pendingBefore, deliveringBefore time.Time, limit int) (list []*WebhookDeliveryEntity, err error) {
	err = m.tx(ctx).Model(&WebhookDeliveryEntity{}).
		Where("(status = ? AND create_time < ?) OR (status = ? AND update_time < ?)",
			webhookenums.DeliveryStatusPending, pendingBefore,
			webhookenums.DeliveryStatusDelivering, deliveringBefore).
		Order("create_time ASC, id ASC").Limit(limit).Find(&list).Error
	return list, err
}

func (m *WebhookModel) CountDeliveriesByStatus(ctx context.Context, status string) (int64, error) {
	var n int64
	err := m.tx(ctx).Model(&WebhookDeliveryEntity{}).Where("status = ?", status).Count(&n).Error
	return n, err
}

type deliveryFilter struct {
	EndpointID uint64
	EventType  string
	Status     string
}

func (f deliveryFilter) apply(q *gorm.DB) *gorm.DB {
	if f.EndpointID != 0 {
		q = q.Where("endpoint_id = ?", f.EndpointID)
	}
	if f.EventType != "" {
		q = q.Where("event_type = ?", f.EventType)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	return q
}

func (m *WebhookModel) ListDeliveries(ctx context.Context, endpointID uint64, eventType, status string, offset, limit int) (list []*WebhookDeliveryEntity, err error) {
	f := deliveryFilter{EndpointID: endpointID, EventType: eventType, Status: status}
	err = f.apply(m.tx(ctx).Model(&WebhookDeliveryEntity{})).
		Order("create_time DESC, id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, err
}

func (m *WebhookModel) CountDeliveries(ctx context.Context, endpointID uint64, eventType, status string) (n int64, err error) {
	f := deliveryFilter{EndpointID: endpointID, EventType: eventType, Status: status}
	err = f.apply(m.tx(ctx).Model(&WebhookDeliveryEntity{})).Count(&n).Error
	return n, err
}

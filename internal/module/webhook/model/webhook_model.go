package webhookmodel

// webhook_model.go — webhook 域的表访问单元（端点白名单 + 投递日志两张表）。
//
// 与其它模块 model 同一形状：只暴露具名方法，不暴露 DB 句柄；
// service 层经这些方法完成全部持久化。

import (
	"context"

	"gorm.io/gorm"
)

// WebhookEndpointEntity 外部集成端点（管理员预注册的白名单条目）。
//
// SecretCipher 存 HMAC 签名密钥的加密文本（pkg/crypto，装配期注入 app.secret 加密），
// 明文密钥永不落库、不出 service 层。
type WebhookEndpointEntity struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	EventType    string `gorm:"column:event_type;size:128;not null;index"`
	TargetURL    string `gorm:"column:target_url;size:1024;not null"`
	SecretCipher string `gorm:"column:secret_cipher;size:512;not null"`
	Description  string `gorm:"column:description;size:512"`
	Status       int    `gorm:"column:status;not null;default:1;index"`
	CreatedAt    int64  `gorm:"column:create_time;not null"`
	UpdatedAt    int64  `gorm:"column:update_time;not null"`
}

// TableName 表名。
func (WebhookEndpointEntity) TableName() string { return "webhook_endpoints" }

// WebhookDeliveryEntity 一次投递的日志（入队即建 pending 行，worker 回写结果）。
type WebhookDeliveryEntity struct {
	ID             uint64 `gorm:"primaryKey;autoIncrement"`
	EndpointID     uint64 `gorm:"column:endpoint_id;not null;index"`
	EventType      string `gorm:"column:event_type;size:128;not null;index"`
	Payload        string `gorm:"column:payload;type:text;not null"`
	Status         string `gorm:"column:status;size:16;not null;default:'pending';index"`
	Attempts       int    `gorm:"column:attempts;not null;default:0"`
	ResponseStatus int    `gorm:"column:response_status"`
	LastError      string `gorm:"column:last_error;size:1024"`
	CreatedAt      int64  `gorm:"column:create_time;not null"`
	UpdatedAt      int64  `gorm:"column:update_time;not null"`
}

// TableName 表名。
func (WebhookDeliveryEntity) TableName() string { return "webhook_deliveries" }

// WebhookModel webhook 域的表访问单元（管本模块 2 张表）。
type WebhookModel struct{ db *gorm.DB }

// NewWebhookModel 构造。
func NewWebhookModel(db *gorm.DB) *WebhookModel { return &WebhookModel{db: db} }

// tx 内部句柄。
func (m *WebhookModel) tx(ctx context.Context) *gorm.DB { return m.db.WithContext(ctx) }

// ---- 端点 ----

// CreateEndpoint 新建端点。
func (m *WebhookModel) CreateEndpoint(ctx context.Context, e *WebhookEndpointEntity) error {
	return m.tx(ctx).Create(e).Error
}

// GetEndpoint 按主键取端点。
func (m *WebhookModel) GetEndpoint(ctx context.Context, id uint64) (e *WebhookEndpointEntity, err error) {
	e = &WebhookEndpointEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// ListEndpoints 列出端点（onlyEnabled 为 true 时只取启用项）。
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

// UpdateEndpoint 更新端点字段。
func (m *WebhookModel) UpdateEndpoint(ctx context.Context, id uint64, fields map[string]any) error {
	return m.tx(ctx).Model(&WebhookEndpointEntity{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteEndpoint 删除端点。
func (m *WebhookModel) DeleteEndpoint(ctx context.Context, id uint64) error {
	return m.tx(ctx).Where("id = ?", id).Delete(&WebhookEndpointEntity{}).Error
}

// ---- 投递 ----

// CreateDelivery 新建投递日志。
func (m *WebhookModel) CreateDelivery(ctx context.Context, e *WebhookDeliveryEntity) error {
	return m.tx(ctx).Create(e).Error
}

// GetDelivery 按主键取投递。
func (m *WebhookModel) GetDelivery(ctx context.Context, id uint64) (e *WebhookDeliveryEntity, err error) {
	e = &WebhookDeliveryEntity{}
	err = m.tx(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// UpdateDeliveryResult 回写投递结果。
func (m *WebhookModel) UpdateDeliveryResult(ctx context.Context, id uint64, fields map[string]any) error {
	return m.tx(ctx).Model(&WebhookDeliveryEntity{}).Where("id = ?", id).Updates(fields).Error
}

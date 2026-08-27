// Package pubmodel 实现 publication 模块 page_routes 与 publication_receipts
// 表持久化：URL 占用状态流转与发布回执记录。
package pubmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const (
	tableNamePageRoutes = "page_routes"
	tableNameReceipts   = "publication_receipts"
)

// 路由占用类型（docs/02-domain.md §page_routes）。
const (
	RouteReserved = "reserved"
	RouteActive   = "active"
	RouteRedirect = "redirect"
)

// 回执状态（docs/03-pipeline.md §9 publication_receipts）。
const (
	ReceiptPending    = "pending"
	ReceiptCommitted  = "committed"
	ReceiptRolledBack = "rolled_back"
)

// RouteEntity 对应 page_routes 表。
type RouteEntity struct {
	ProjectID      string    `gorm:"column:project_id;type:uuid;primaryKey"`
	Path           string    `gorm:"column:path;type:text;primaryKey"`
	PageID         *string   `gorm:"column:page_id;type:uuid"`
	PresentationID *string   `gorm:"column:presentation_id;type:uuid"`
	RouteKind      string    `gorm:"column:route_kind;type:text;not null"`
	ArtifactID     *string   `gorm:"column:artifact_id;type:uuid"`
	UpdatedAt      time.Time `gorm:"column:updated_at;not null"`
}

func (RouteEntity) TableName() string { return tableNamePageRoutes }

// ReceiptEntity 对应 publication_receipts 表：发布回执（故障恢复依据）。
type ReceiptEntity struct {
	ID           string          `gorm:"column:id;type:uuid;primaryKey"`
	SourceType   string          `gorm:"column:source_type;type:text;not null"`
	SourceID     string          `gorm:"column:source_id;type:uuid;not null"`
	Action       string          `gorm:"column:action;type:text;not null"`
	Path         string          `gorm:"column:path;type:text;not null"`
	FromArtifact *string         `gorm:"column:from_artifact_id;type:uuid"`
	ToArtifact   *string         `gorm:"column:to_artifact_id;type:uuid"`
	ReceiptState string          `gorm:"column:receipt_state;type:text;not null"`
	ReceiptData  json.RawMessage `gorm:"column:receipt_data;type:jsonb;not null"`
	CreatedAt    time.Time       `gorm:"column:created_at;not null"`
	CompletedAt  *time.Time      `gorm:"column:completed_at"`
}

func (ReceiptEntity) TableName() string { return tableNameReceipts }

// Model 封装 publication 表数据访问。
type Model struct {
	db *gorm.DB
}

// NewPublicationModel 创建 Publication Model。
func NewPublicationModel(db *gorm.DB) *Model { return &Model{db: db} }

// RouteDB 返回已绑定 page_routes 表的 GORM 实例。
func (m *Model) RouteDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&RouteEntity{})
}

// ReceiptDB 返回已绑定 publication_receipts 表的 GORM 实例。
func (m *Model) ReceiptDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ReceiptEntity{})
}

// Transaction 在数据库事务中执行给定函数；路由与回执必须原子提交。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// GetRoute 按 (projectID, path) 查询路由占用。
func (m *Model) GetRoute(ctx context.Context, projectID, path string) (e *RouteEntity, err error) {
	e = &RouteEntity{}
	if err = m.RouteDB(ctx).Where("project_id = ? AND path = ?", projectID, path).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// ListPendingReceipts 读取全部 pending 回执（恢复流程扫描）。
func (m *Model) ListPendingReceipts(ctx context.Context) (list []ReceiptEntity, err error) {
	err = m.ReceiptDB(ctx).Where("receipt_state = ?", ReceiptPending).Order("created_at ASC").Find(&list).Error
	return list, err
}

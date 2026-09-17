package mediamodel

// media_variant_model.go — sys_media_variant 表访问单元（Repository）:
// 仅做单表 CRUD 与单表聚合，业务规则（状态机、生成编排）在 service 层。

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const tableNameSysMediaVariant = "sys_media_variant"

// 变体类型常量（与迁移 048_media_variant.sql 的取值一致）。
const (
	VariantTypeThumb  = "thumb"
	VariantTypeMedium = "medium"
	VariantTypeWebp   = "webp"
)

// 变体生成状态常量（varchar，服务端状态机）。
const (
	VariantStatusPending    = "pending"
	VariantStatusProcessing = "processing"
	VariantStatusReady      = "ready"
	VariantStatusFailed     = "failed"
)

// VariantTypes 返回全部受支持的变体类型（生成顺序固定）。
func VariantTypes() []string {
	return []string{VariantTypeThumb, VariantTypeMedium, VariantTypeWebp}
}

// MediaVariantEntity 对应 sys_media_variant 表。
type MediaVariantEntity struct {
	ID           uint64     `gorm:"column:id;primaryKey"`
	AttachmentID uint64     `gorm:"column:attachment_id"`
	VariantType  string     `gorm:"column:variant_type"`
	FilePath     string     `gorm:"column:file_path"`
	Width        *int       `gorm:"column:width"`
	Height       *int       `gorm:"column:height"`
	FileSize     int64      `gorm:"column:file_size"`
	MimeType     *string    `gorm:"column:mime_type"`
	Status       string     `gorm:"column:status;default:pending"`
	CreateTime   time.Time  `gorm:"column:create_time;autoCreateTime"`
	UpdateTime   *time.Time `gorm:"column:update_time"`
}

func (MediaVariantEntity) TableName() string { return tableNameSysMediaVariant }

// MediaVariantModel 封装 sys_media_variant 表的数据访问。
type MediaVariantModel struct {
	db *gorm.DB
}

// NewMediaVariantModel 创建变体模型。
func NewMediaVariantModel(db *gorm.DB) *MediaVariantModel {
	return &MediaVariantModel{db: db}
}

// varDB 返回绑定 MediaVariantEntity 的 GORM DB 实例。
func (m *MediaVariantModel) varDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&MediaVariantEntity{})
}

// Create 新增一条变体记录。
func (m *MediaVariantModel) Create(ctx context.Context, e *MediaVariantEntity) error {
	return m.varDB(ctx).Create(e).Error
}

// CreateBatch 批量新增变体记录（同一事务内插入，上传登记使用）。
func (m *MediaVariantModel) CreateBatch(ctx context.Context, list []*MediaVariantEntity) error {
	if len(list) == 0 {
		return nil
	}
	return m.varDB(ctx).Create(&list).Error
}

// DeleteByAttachment 物理删除指定附件的全部变体记录。
// 生成入口「重跑先清旧记录」的幂等动作；附件硬删除时由 FK ON DELETE CASCADE 兜底。
func (m *MediaVariantModel) DeleteByAttachment(ctx context.Context, attachmentID uint64) error {
	return m.varDB(ctx).Where("attachment_id = ?", attachmentID).Delete(&MediaVariantEntity{}).Error
}

// Update 按 ID 更新变体字段（状态流转 / 生成结果回填）。
func (m *MediaVariantModel) Update(ctx context.Context, id uint64, updates map[string]any) error {
	return m.varDB(ctx).Where("id = ?", id).Updates(updates).Error
}

// GetByFilePath 按变体存储相对路径查询变体记录（构建期从产物 URL 反查附件用：
// 页面里可能直接引用 <stem>_thumb.jpg 这类变体地址）。
func (m *MediaVariantModel) GetByFilePath(ctx context.Context, filePath string) (*MediaVariantEntity, error) {
	var e MediaVariantEntity
	err := m.varDB(ctx).
		Where("file_path = ? OR file_path = ?", filePath, "/"+filePath).
		Order("id ASC").
		First(&e).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ListByAttachment 查询指定附件的全部变体记录，按 thumb/medium/webp 固定顺序。
func (m *MediaVariantModel) ListByAttachment(ctx context.Context, attachmentID uint64) ([]MediaVariantEntity, error) {
	var list []MediaVariantEntity
	err := m.varDB(ctx).Where("attachment_id = ?", attachmentID).Order("id ASC").Find(&list).Error
	return list, err
}

// ListByAttachmentIDs 批量查询多个附件的变体记录（列表页一次 IN 查询，避免 N+1），
// 返回 attachment_id → 变体列表 的映射，缺失的附件不出现在映射中。
func (m *MediaVariantModel) ListByAttachmentIDs(ctx context.Context, ids []uint64) (map[uint64][]MediaVariantEntity, error) {
	out := make(map[uint64][]MediaVariantEntity)
	if len(ids) == 0 {
		return out, nil
	}
	var list []MediaVariantEntity
	if err := m.varDB(ctx).Where("attachment_id IN ?", ids).Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	for _, e := range list {
		out[e.AttachmentID] = append(out[e.AttachmentID], e)
	}
	return out, nil
}

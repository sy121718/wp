package mediamodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const tableNameSysMediaVariant = "sys_media_variant"

const (
	VariantTypeThumb = "thumb"

	VariantTypeSmall  = "small"
	VariantTypeMedium = "medium"

	VariantTypeFull = "full"
)

const (
	VariantStatusPending    = "pending"
	VariantStatusProcessing = "processing"
	VariantStatusReady      = "ready"
	VariantStatusFailed     = "failed"
)

func VariantTypes() []string {

	return []string{VariantTypeThumb, VariantTypeSmall, VariantTypeMedium, VariantTypeFull}
}

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

type MediaVariantModel struct {
	db *gorm.DB
}

func NewMediaVariantModel(db *gorm.DB) *MediaVariantModel {
	return &MediaVariantModel{db: db}
}

func (m *MediaVariantModel) varDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&MediaVariantEntity{})
}

func (m *MediaVariantModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *MediaVariantModel) Create(ctx context.Context, e *MediaVariantEntity) error {
	return m.varDB(ctx).Create(e).Error
}

func (m *MediaVariantModel) CreateBatch(ctx context.Context, list []*MediaVariantEntity) error {
	if len(list) == 0 {
		return nil
	}
	return m.varDB(ctx).Create(&list).Error
}

func (m *MediaVariantModel) DeleteByAttachment(ctx context.Context, attachmentID uint64) error {
	return m.varDB(ctx).Where("attachment_id = ?", attachmentID).Delete(&MediaVariantEntity{}).Error
}

func (m *MediaVariantModel) DeleteByAttachmentTx(ctx context.Context, tx *gorm.DB, attachmentID uint64) error {
	return tx.WithContext(ctx).Model(&MediaVariantEntity{}).Where("attachment_id = ?", attachmentID).Delete(&MediaVariantEntity{}).Error
}

func (m *MediaVariantModel) CreateBatchTx(ctx context.Context, tx *gorm.DB, list []*MediaVariantEntity) error {
	if len(list) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Model(&MediaVariantEntity{}).Create(&list).Error
}

func (m *MediaVariantModel) ListForAudit(ctx context.Context, limit int) ([]MediaVariantEntity, error) {
	var list []MediaVariantEntity
	err := m.varDB(ctx).Order("id ASC").Limit(limit).Find(&list).Error
	return list, err
}

func (m *MediaVariantModel) Update(ctx context.Context, id uint64, updates map[string]any) error {
	return m.varDB(ctx).Where("id = ?", id).Updates(updates).Error
}

func (m *MediaVariantModel) UpdateTx(ctx context.Context, tx *gorm.DB, id uint64, updates map[string]any) error {
	return tx.WithContext(ctx).Model(&MediaVariantEntity{}).Where("id = ?", id).Updates(updates).Error
}

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

func (m *MediaVariantModel) ListByAttachment(ctx context.Context, attachmentID uint64) ([]MediaVariantEntity, error) {
	var list []MediaVariantEntity
	err := m.varDB(ctx).Where("attachment_id = ?", attachmentID).Order("id ASC").Find(&list).Error
	return list, err
}

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

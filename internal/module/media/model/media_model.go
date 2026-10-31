package mediamodel

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"regexp"
	"time"

	"go_wp/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	tableNameSysAttachment   = "sys_attachment"
	tableNameSysFileCategory = "sys_file_category"
)

const (
	AttachmentStatusDisabled = 0
	AttachmentStatusEnabled  = 1
)

type AttachmentEntity struct {
	ID          uint64  `gorm:"column:id;primaryKey"`
	CategoryID  *uint64 `gorm:"column:category_id"`
	FileName    string  `gorm:"column:file_name"`
	FilePath    string  `gorm:"column:file_path"`
	FileSize    int64   `gorm:"column:file_size"`
	FileType    string  `gorm:"column:file_type"`
	MimeType    *string `gorm:"column:mime_type"`
	StorageType string  `gorm:"column:storage_type;default:local"`
	StoragePath *string `gorm:"column:storage_path"`
	URL         *string `gorm:"column:url"`
	MD5         *string `gorm:"column:md5"`
	ExtraInfo   *string `gorm:"column:extra_info"`

	Generation int        `gorm:"column:generation;not null;default:1"`
	Status     int        `gorm:"column:status;default:1"`
	CreateBy   *uint64    `gorm:"column:create_by"`
	UpdateBy   *uint64    `gorm:"column:update_by"`
	CreateTime time.Time  `gorm:"column:create_time;autoCreateTime"`
	UpdateTime *time.Time `gorm:"column:update_time"`
}

func (AttachmentEntity) TableName() string { return tableNameSysAttachment }

type FileCategoryEntity struct {
	ID           uint64     `gorm:"column:id;primaryKey"`
	CategoryName string     `gorm:"column:category_name"`
	CategoryCode string     `gorm:"column:category_code;uniqueIndex"`
	ParentID     uint64     `gorm:"column:parent_id;default:0"`
	SortOrder    int        `gorm:"column:sort_order;default:0"`
	Icon         *string    `gorm:"column:icon"`
	Status       int        `gorm:"column:status;default:1"`
	CreateBy     *uint64    `gorm:"column:create_by"`
	UpdateBy     *uint64    `gorm:"column:update_by"`
	CreateTime   *time.Time `gorm:"column:create_time"`
	UpdateTime   *time.Time `gorm:"column:update_time"`
}

func (FileCategoryEntity) TableName() string { return tableNameSysFileCategory }

type AttachmentModel struct {
	db *gorm.DB
}

type FileCategoryModel struct {
	db *gorm.DB
}

func NewAttachmentModel(db *gorm.DB) *AttachmentModel {
	return &AttachmentModel{db: db}
}

func NewFileCategoryModel(db *gorm.DB) *FileCategoryModel {
	return &FileCategoryModel{db: db}
}

func (m *AttachmentModel) attrDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AttachmentEntity{})
}

func (m *FileCategoryModel) catDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&FileCategoryEntity{})
}

func (m *AttachmentModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *AttachmentModel) Create(ctx context.Context, e *AttachmentEntity) error {
	return m.attrDB(ctx).Create(e).Error
}

func (m *AttachmentModel) GetByID(ctx context.Context, id uint64) (*AttachmentEntity, error) {
	var e AttachmentEntity
	err := m.attrDB(ctx).Where("id = ? AND status = ?", id, AttachmentStatusEnabled).First(&e).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (m *AttachmentModel) GetByFilePath(ctx context.Context, filePath string) (*AttachmentEntity, error) {
	var e AttachmentEntity
	err := m.attrDB(ctx).
		Where("status = ?", AttachmentStatusEnabled).
		Where("file_path = ? OR file_path = ?", filePath, "/"+filePath).
		First(&e).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (m *AttachmentModel) GetByMD5AndType(ctx context.Context, md5 string, fileType string) (*AttachmentEntity, error) {
	var e AttachmentEntity
	err := m.attrDB(ctx).
		Where("md5 = ? AND file_type = ? AND status = ?", md5, fileType, AttachmentStatusEnabled).
		Order("id ASC").
		First(&e).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (m *AttachmentModel) ReplaceContentMeta(ctx context.Context, id uint64, md5hex string, size int64, mimeType string, updatedAt time.Time) (int, error) {

	var updated AttachmentEntity
	err := m.db.WithContext(ctx).
		Model(&updated).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "generation"}}}).
		Where("id = ?", id).
		Updates(map[string]any{
			"generation":  gorm.Expr("generation + 1"),
			"md5":         md5hex,
			"file_size":   size,
			"mime_type":   mimeType,
			"update_time": updatedAt,
		}).Error
	if err != nil {
		return 0, err
	}
	return updated.Generation, nil
}

func (m *AttachmentModel) HardDelete(ctx context.Context, id uint64) error {
	return m.attrDB(ctx).Where("id = ?", id).Delete(&AttachmentEntity{}).Error
}

func (m *AttachmentModel) List(ctx context.Context, fileType string, categoryID *uint64, uncategorized bool, search string, offset, limit int) ([]AttachmentEntity, int64, error) {
	q := m.attrDB(ctx).Where("status = ?", AttachmentStatusEnabled)
	if fileType != "" {
		q = q.Where("file_type = ?", fileType)
	}
	if categoryID != nil && *categoryID > 0 {
		q = q.Where("category_id = ?", *categoryID)
	}
	if uncategorized {
		q = q.Where("category_id IS NULL")
	}
	if search != "" {

		q = q.Where("file_name ILIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(search)+"%")
	}

	if uncategorized {
		q = q.Where("category_id IS NULL")
	}

	return m.listPage(q, offset, limit, nil, 0)
}

func (m *AttachmentModel) ListAfter(ctx context.Context, fileType string, categoryID *uint64, uncategorized bool, search string, after time.Time, afterID uint64, limit int) ([]AttachmentEntity, int64, error) {
	q := m.attrDB(ctx).Where("status = ?", AttachmentStatusEnabled)
	if fileType != "" {
		q = q.Where("file_type = ?", fileType)
	}
	if categoryID != nil && *categoryID > 0 {
		q = q.Where("category_id = ?", *categoryID)
	}
	if uncategorized {
		q = q.Where("category_id IS NULL")
	}
	if search != "" {
		q = q.Where("file_name ILIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(search)+"%")
	}
	return m.listPage(q, 0, limit, &after, afterID)
}

func (m *AttachmentModel) listPage(q *gorm.DB, offset, limit int, after *time.Time, afterID uint64) ([]AttachmentEntity, int64, error) {
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if after != nil {
		q = q.Where("(create_time, id) < (?, ?)", *after, afterID)
	}
	var list []AttachmentEntity
	if err := q.Order("create_time DESC, id DESC").Offset(offset).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (m *AttachmentModel) Delete(ctx context.Context, id uint64) error {
	return m.attrDB(ctx).Where("id = ?", id).Update("status", AttachmentStatusDisabled).Error
}

func (m *FileCategoryModel) ListAll(ctx context.Context) ([]FileCategoryEntity, error) {
	var list []FileCategoryEntity
	err := m.catDB(ctx).Where("status = ?", 1).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

func (m *FileCategoryModel) CreateCategory(ctx context.Context, e *FileCategoryEntity) error {
	return m.catDB(ctx).Create(e).Error
}

func (m *FileCategoryModel) GetCategory(ctx context.Context, id uint64) (*FileCategoryEntity, error) {
	e := &FileCategoryEntity{}
	if err := m.catDB(ctx).Where("id = ? AND status = ?", id, 1).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

func (m *FileCategoryModel) UpdateCategory(ctx context.Context, id uint64, updates map[string]any) error {
	return m.catDB(ctx).Where("id = ?", id).Updates(updates).Error
}

func (m *FileCategoryModel) HasChildren(ctx context.Context, id uint64) (bool, error) {
	var n int64
	err := m.catDB(ctx).Where("parent_id = ? AND status = ?", id, 1).Count(&n).Error
	return n > 0, err
}

func (m *FileCategoryModel) DeleteCategory(ctx context.Context, id uint64) error {
	return m.catDB(ctx).Where("id = ?", id).Update("status", 0).Error
}

func (m *AttachmentModel) AttachmentUpdate(ctx context.Context, id uint64, updates map[string]any) error {
	return m.attrDB(ctx).Where("id = ?", id).Updates(updates).Error
}

func (m *AttachmentModel) AttachmentUpdateTx(ctx context.Context, tx *gorm.DB, id uint64, updates map[string]any) error {
	return tx.WithContext(ctx).Model(&AttachmentEntity{}).Where("id = ?", id).Updates(updates).Error
}

func (m *AttachmentModel) ListForAudit(ctx context.Context, limit int) ([]AttachmentEntity, error) {
	var list []AttachmentEntity
	err := m.attrDB(ctx).Order("id ASC").Limit(limit).Find(&list).Error
	return list, err
}

func (m *AttachmentModel) CountForAudit(ctx context.Context) (int64, error) {
	var n int64
	err := m.attrDB(ctx).Count(&n).Error
	return n, err
}

var extraKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

func (m *AttachmentModel) MergeAttachmentExtra(ctx context.Context, id uint64, set map[string]any, del []string) error {
	setJSON := "{}"
	if len(set) > 0 {
		raw, err := json.Marshal(set)
		if err != nil {
			return err
		}
		setJSON = string(raw)
	}

	base := "COALESCE(NULLIF(extra_info, 'null'::jsonb), '{}'::jsonb)"

	expr := "CASE WHEN jsonb_typeof(extra_info) IN ('array','string','number','boolean') THEN extra_info ELSE ((" + base + " || ?::jsonb)"
	for _, k := range del {

		if !extraKeyRe.MatchString(k) {
			return fmt.Errorf("非法的 ExtraInfo 键名: %q", k)
		}
		expr += " - '" + k + "'::text"
	}
	expr += ") END"
	return m.attrDB(ctx).Where("id = ?", id).
		Update("extra_info", gorm.Expr(expr, setJSON)).Error
}

func (m *AttachmentModel) DetachAttachments(ctx context.Context, categoryID uint64) error {
	return m.attrDB(ctx).Where("category_id = ?", categoryID).Update("category_id", nil).Error
}

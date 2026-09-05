// Package mediamodel 媒体模块的数据库模型层，封装 sys_attachment 和 sys_file_category 表的 CRUD 操作。
package mediamodel

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	tableNameSysAttachment   = "sys_attachment"
	tableNameSysFileCategory = "sys_file_category"
)

const (
	AttachmentStatusDisabled = 0
	AttachmentStatusEnabled  = 1
)

// AttachmentEntity 对应 sys_attachment 表。
type AttachmentEntity struct {
	ID          uint64     `gorm:"column:id;primaryKey"`
	CategoryID  *uint64    `gorm:"column:category_id"`
	FileName    string     `gorm:"column:file_name;type:varchar(255)"`
	FilePath    string     `gorm:"column:file_path;type:varchar(500)"`
	FileSize    int64      `gorm:"column:file_size"`
	FileType    string     `gorm:"column:file_type;type:varchar(50)"`
	MimeType    *string    `gorm:"column:mime_type;type:varchar(100)"`
	StorageType string     `gorm:"column:storage_type;type:varchar(50);default:local"`
	StoragePath *string    `gorm:"column:storage_path;type:varchar(500)"`
	URL         *string    `gorm:"column:url;type:varchar(500)"`
	MD5         *string    `gorm:"column:md5;type:varchar(32)"`
	ExtraInfo   *string    `gorm:"column:extra_info;type:json"`
	Status      int        `gorm:"column:status;default:1"`
	CreateBy    *uint64    `gorm:"column:create_by"`
	UpdateBy    *uint64    `gorm:"column:update_by"`
	CreateTime  time.Time  `gorm:"column:create_time;autoCreateTime"`
	UpdateTime  *time.Time `gorm:"column:update_time"`
}

func (AttachmentEntity) TableName() string { return tableNameSysAttachment }

// FileCategoryEntity 对应 sys_file_category 表。
type FileCategoryEntity struct {
	ID           uint64     `gorm:"column:id;primaryKey"`
	CategoryName string     `gorm:"column:category_name;type:varchar(100)"`
	CategoryCode string     `gorm:"column:category_code;type:varchar(50);uniqueIndex"`
	ParentID     uint64     `gorm:"column:parent_id;default:0"`
	SortOrder    int        `gorm:"column:sort_order;default:0"`
	Icon         *string    `gorm:"column:icon;type:varchar(50)"`
	Status       int        `gorm:"column:status;default:1"`
	CreateBy     *uint64    `gorm:"column:create_by"`
	UpdateBy     *uint64    `gorm:"column:update_by"`
	CreateTime   *time.Time `gorm:"column:create_time"`
	UpdateTime   *time.Time `gorm:"column:update_time"`
}

func (FileCategoryEntity) TableName() string { return tableNameSysFileCategory }

// AttachmentModel 封装 sys_attachment 表的数据访问。
type AttachmentModel struct {
	db *gorm.DB
}

// FileCategoryModel 封装 sys_file_category 表的数据访问。
type FileCategoryModel struct {
	db *gorm.DB
}

// NewAttachmentModel 创建附件模型。
func NewAttachmentModel(db *gorm.DB) *AttachmentModel {
	return &AttachmentModel{db: db}
}

// NewFileCategoryModel 创建分类模型。
func NewFileCategoryModel(db *gorm.DB) *FileCategoryModel {
	return &FileCategoryModel{db: db}
}

// attrDB 返回绑定 AttachmentEntity 的 GORM DB 实例。
func (m *AttachmentModel) attrDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AttachmentEntity{})
}

// catDB 返回绑定 FileCategoryEntity 的 GORM DB 实例。
func (m *FileCategoryModel) catDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&FileCategoryEntity{})
}

// --- AttachmentModel 方法 ---

// Create 新增一条附件记录。
func (m *AttachmentModel) Create(ctx context.Context, e *AttachmentEntity) error {
	return m.attrDB(ctx).Create(e).Error
}

// GetByID 根据 ID 查询附件（仅启用记录，软删除后不可见）。
func (m *AttachmentModel) GetByID(ctx context.Context, id uint64) (*AttachmentEntity, error) {
	var e AttachmentEntity
	err := m.attrDB(ctx).Where("id = ? AND status = ?", id, AttachmentStatusEnabled).First(&e).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// List 分页查询附件，支持按文件类型和分类过滤。
func (m *AttachmentModel) List(ctx context.Context, fileType string, categoryID *uint64, search string, offset, limit int) ([]AttachmentEntity, int64, error) {
	q := m.attrDB(ctx).Where("status = ?", AttachmentStatusEnabled)
	if fileType != "" {
		q = q.Where("file_type = ?", fileType)
	}
	if categoryID != nil && *categoryID > 0 {
		q = q.Where("category_id = ?", *categoryID)
	}
	if search != "" {
		// LIKE 通配符转义：_ / % 按字面匹配（ESCAPE '\'）。
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		q = q.Where("file_name LIKE ? ESCAPE '\\'", "%"+escaped+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []AttachmentEntity
	if err := q.Order("create_time DESC").Offset(offset).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// Delete 软删除附件（设置 status=0）。
func (m *AttachmentModel) Delete(ctx context.Context, id uint64) error {
	return m.attrDB(ctx).Where("id = ?", id).Update("status", AttachmentStatusDisabled).Error
}

// --- FileCategoryModel 方法 ---

// ListAll 查询所有启用的分类，按 sort_order, id 排序。
func (m *FileCategoryModel) ListAll(ctx context.Context) ([]FileCategoryEntity, error) {
	var list []FileCategoryEntity
	err := m.catDB(ctx).Where("status = ?", 1).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

// --- 分类 CRUD（媒体库左树管理） ---

// CreateCategory 新建分类（ParentID=0 为顶级）。
func (m *FileCategoryModel) CreateCategory(ctx context.Context, e *FileCategoryEntity) error {
	return m.catDB(ctx).Create(e).Error
}

// GetCategory 按 ID 查询分类（仅启用记录，软删除分类不可作父级/目标）。
func (m *FileCategoryModel) GetCategory(ctx context.Context, id uint64) (*FileCategoryEntity, error) {
	e := &FileCategoryEntity{}
	if err := m.catDB(ctx).Where("id = ? AND status = ?", id, 1).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// UpdateCategory 更新分类（仅非 nil 字段）。
func (m *FileCategoryModel) UpdateCategory(ctx context.Context, id uint64, updates map[string]any) error {
	return m.catDB(ctx).Where("id = ?", id).Updates(updates).Error
}

// HasChildren 判断分类是否存在启用子级。
func (m *FileCategoryModel) HasChildren(ctx context.Context, id uint64) (bool, error) {
	var n int64
	err := m.catDB(ctx).Where("parent_id = ? AND status = ?", id, 1).Count(&n).Error
	return n > 0, err
}

// DeleteCategory 软删除分类（status=0）。
func (m *FileCategoryModel) DeleteCategory(ctx context.Context, id uint64) error {
	return m.catDB(ctx).Where("id = ?", id).Update("status", 0).Error
}

// AttachmentUpdate 更新附件字段（文件名 / 分类 / ExtraInfo JSON）。
func (m *AttachmentModel) AttachmentUpdate(ctx context.Context, id uint64, updates map[string]any) error {
	return m.attrDB(ctx).Where("id = ?", id).Updates(updates).Error
}

// DetachAttachments 把分类下的全部附件移入未分类（category_id=NULL，分类删除前的级联动作）。
func (m *AttachmentModel) DetachAttachments(ctx context.Context, categoryID uint64) error {
	return m.attrDB(ctx).Where("category_id = ?", categoryID).Update("category_id", nil).Error
}

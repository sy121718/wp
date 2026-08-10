// Package menumodel 菜单模块数据模型与数据访问。
package menumodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const tableNameSysMenus = "sys_menus"

const (
	MenuTypeDirectory = 1 // 目录
	MenuTypeMenu      = 2 // 菜单
	MenuTypeButton    = 3 // 按钮
	MenuTypeIframe    = 4 // iframe
	MenuTypeExternal  = 5 // 外链
)

const (
	MenuStatusDisabled = 0
	MenuStatusEnabled  = 1
)

// MenuEntity 对应 sys_menus 表。
type MenuEntity struct {
	ID             uint64     `gorm:"column:id;primaryKey"`
	PermissionCode *string    `gorm:"column:permission_code;type:varchar(100)"`
	Title          string     `gorm:"column:title;type:varchar(50)"`
	ParentID       uint64     `gorm:"column:parent_id;default:0"`
	Type           int        `gorm:"column:type;default:2"`
	Path           string     `gorm:"column:path;type:varchar(100)"`
	Component      string     `gorm:"column:component;type:varchar(255)"`
	ExternalURL    string     `gorm:"column:external_url;type:varchar(300)"`
	Icon           string     `gorm:"column:icon;type:varchar(50)"`
	Status         int        `gorm:"column:status;default:1"`
	IsHidden       int        `gorm:"column:is_hidden;default:0"`
	IsPublic       int        `gorm:"column:is_public;default:0"`
	IsSystem       int        `gorm:"column:is_system;default:0"`
	SortOrder      int        `gorm:"column:sort_order;default:0"`
	Remark         *string    `gorm:"column:remark;type:varchar(200)"`
	CreateBy       uint64     `gorm:"column:create_by;type:bigint unsigned"`
	CreateTime     *time.Time `gorm:"column:create_time;type:datetime(3)"`
	UpdateBy       uint64     `gorm:"column:update_by;type:bigint unsigned"`
	UpdateTime     *time.Time `gorm:"column:update_time;type:datetime(3)"`
	DeletedTime    *time.Time `gorm:"column:deleted_time;type:datetime(3)"`
}

// TableName 返回 sys_menus 表名。
func (MenuEntity) TableName() string {
	return tableNameSysMenus
}

// MenuModel 菜单数据访问。
type MenuModel struct {
	db *gorm.DB
}

// NewMenuModel 创建菜单数据访问实例。
func NewMenuModel(db *gorm.DB) *MenuModel {
	return &MenuModel{db: db}
}

// DB 返回绑定当前菜单表的 GORM 查询上下文。
func (m *MenuModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&MenuEntity{})
}

// BeforeCreate 创建前补齐时间。
func (e *MenuEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	e.CreateTime = &now
	e.UpdateTime = &now
	return nil
}

// BeforeUpdate 更新前刷新时间。
func (e *MenuEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	e.UpdateTime = &now
	return nil
}

// GetByID 根据 ID 查询菜单，不存在返回 nil。
func (m *MenuModel) GetByID(ctx context.Context, id uint64) (*MenuEntity, error) {
	var entity MenuEntity
	err := m.DB(ctx).Where("id = ? AND deleted_time IS NULL", id).First(&entity).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListAll 查询全部未删除菜单，按 sort_order、id 排序。
func (m *MenuModel) ListAll(ctx context.Context) ([]MenuEntity, error) {
	var list []MenuEntity
	err := m.DB(ctx).Where("deleted_time IS NULL").Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

// ListByIDs 按 ID 列表批量查询。
func (m *MenuModel) ListByIDs(ctx context.Context, ids []uint64) ([]MenuEntity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []MenuEntity
	err := m.DB(ctx).Where("id IN ? AND deleted_time IS NULL", ids).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

// CountByPermissionCodes 统计引用指定权限编码的未删除菜单数。
func (m *MenuModel) CountByPermissionCodes(ctx context.Context, codes []string) (count int64, err error) {
	if len(codes) == 0 {
		return 0, nil
	}
	err = m.DB(ctx).
		Where("permission_code IN ? AND deleted_time IS NULL", codes).
		Count(&count).Error
	return count, err
}

// Create 新建菜单。
func (m *MenuModel) Create(ctx context.Context, e *MenuEntity) error {
	return m.DB(ctx).Create(e).Error
}

// Update 更新菜单，并显式持久化允许清空的零值和 NULL 值。
func (m *MenuModel) Update(ctx context.Context, e *MenuEntity) error {
	return m.DB(ctx).
		Where("id = ?", e.ID).
		Select(
			"permission_code",
			"title",
			"parent_id",
			"type",
			"path",
			"component",
			"external_url",
			"icon",
			"status",
			"is_hidden",
			"is_public",
			"sort_order",
			"remark",
			"update_time",
		).
		Updates(e).Error
}

// SoftDelete 软删除（设置 deleted_time）。
func (m *MenuModel) SoftDelete(ctx context.Context, ids []uint64) (int64, error) {
	now := time.Now()
	result := m.DB(ctx).Where("id IN ? AND deleted_time IS NULL", ids).Update("deleted_time", now)
	return result.RowsAffected, result.Error
}

// CountByParentID 统计子菜单数量。
func (m *MenuModel) CountByParentID(ctx context.Context, parentID uint64) (int64, error) {
	var count int64
	err := m.DB(ctx).Where("parent_id = ? AND deleted_time IS NULL", parentID).Count(&count).Error
	return count, err
}

// ListByPermissionCodes 按 permission_code 列表查询菜单（用于检查引用）。
func (m *MenuModel) ListByPermissionCodes(ctx context.Context, codes []string) ([]MenuEntity, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	var list []MenuEntity
	err := m.DB(ctx).Where("permission_code IN ? AND deleted_time IS NULL", codes).Find(&list).Error
	return list, err
}

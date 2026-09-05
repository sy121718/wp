// Package adminmodel 合并后的统一模型包。
package adminmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const tableNameSysRole = "sys_role"

const (
	RoleStatusDisabled = 0
	RoleStatusEnabled  = 1
)

// RoleEntity 对应 sys_role 表。
type RoleEntity struct {
	ID         uint64     `gorm:"column:id;primaryKey"`
	RoleCode   string     `gorm:"column:role_code;type:varchar(50);uniqueIndex"`
	RoleName   string     `gorm:"column:role_name;type:varchar(100)"`
	// Status 不带 gorm default tag：gorm 对带 default 的字段在零值时会用 DB 默认值
	// 替换并回写 struct，导致显式传入的 status=0（禁用）被改写成 1（启用）。
	// service 层总是显式设置 Status，DB 列 DEFAULT 1 仅兜底直接 SQL 插入。
	Status int `gorm:"column:status;type:tinyint"`
	IsSystem   int        `gorm:"column:is_system;type:tinyint;default:0"`
	SortOrder  int        `gorm:"column:sort_order;default:0"`
	Remark     *string    `gorm:"column:remark;type:varchar(200)"`
	CreateBy   uint64     `gorm:"column:create_by;type:bigint unsigned"`
	CreateTime *time.Time `gorm:"column:create_time;type:datetime(3)"`
	UpdateBy   uint64     `gorm:"column:update_by;type:bigint unsigned"`
	UpdateTime *time.Time `gorm:"column:update_time;type:datetime(3)"`
}

// TableName 返回 sys_role 表名。
func (RoleEntity) TableName() string {
	return tableNameSysRole
}

// RoleModel 角色数据访问。
type RoleModel struct {
	db *gorm.DB
}

// NewRoleModel 创建角色数据访问实例。
func NewRoleModel(db *gorm.DB) *RoleModel {
	return &RoleModel{db: db}
}

// DB 返回绑定当前角色表的 GORM 查询上下文。
func (m *RoleModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&RoleEntity{})
}

// BeforeCreate 创建前补齐时间。
func (e *RoleEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	e.CreateTime = &now
	e.UpdateTime = &now
	return nil
}

// BeforeUpdate 更新前刷新时间。
func (e *RoleEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	e.UpdateTime = &now
	return nil
}

// GetByID 根据 ID 查询角色，不存在返回 nil。
func (m *RoleModel) GetByID(ctx context.Context, id uint64) (*RoleEntity, error) {
	var entity RoleEntity
	err := m.DB(ctx).Where("id = ?", id).First(&entity).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetByCode 根据 role_code 查询角色。
func (m *RoleModel) GetByCode(ctx context.Context, code string) (*RoleEntity, error) {
	var entity RoleEntity
	err := m.DB(ctx).Where("role_code = ?", code).First(&entity).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListAll 分页查询角色列表。
func (m *RoleModel) ListAll(ctx context.Context, page, limit int, keyword string) (int64, []RoleEntity, error) {
	query := m.DB(ctx)
	if keyword != "" {
		query = query.Where("role_code LIKE ? OR role_name LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, nil, err
	}

	var list []RoleEntity
	offset := (page - 1) * limit
	err := query.Order("sort_order ASC, id ASC").Offset(offset).Limit(limit).Find(&list).Error
	return total, list, err
}

// ListByIDs 按 ID 列表批量查询。
func (m *RoleModel) ListByIDs(ctx context.Context, ids []uint64) ([]RoleEntity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []RoleEntity
	err := m.DB(ctx).Where("id IN ?", ids).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

// ListByCodes 按 role_code 列表批量查询。
func (m *RoleModel) ListByCodes(ctx context.Context, codes []string) ([]RoleEntity, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	var list []RoleEntity
	err := m.DB(ctx).Where("role_code IN ?", codes).Find(&list).Error
	return list, err
}

// GetEnabledIDsByCodes 按角色编码查询已启用角色 ID。
func (m *RoleModel) GetEnabledIDsByCodes(ctx context.Context, codes []string) (ids []uint64, err error) {
	if len(codes) == 0 {
		return nil, nil
	}
	err = m.DB(ctx).
		Where("role_code IN ? AND status = ?", codes, RoleStatusEnabled).
		Pluck("id", &ids).Error
	return ids, err
}

// Create 新建角色。
// Status 已无 default tag（见字段注释），零值 0（禁用）可直接落库，无需显式 Select 列。
func (m *RoleModel) Create(ctx context.Context, e *RoleEntity) error {
	return m.DB(ctx).Create(e).Error
}

// Update 更新角色元信息。
// 显式指定列（含 status 零值），否则 gorm 结构体更新会跳过零值字段，
// 导致停用角色（status=0）落库失败、重新启用时状态比对失真。
func (m *RoleModel) Update(ctx context.Context, e *RoleEntity) error {
	return m.DB(ctx).Where("id = ?", e.ID).
		Select("role_name", "status", "sort_order", "remark").Updates(e).Error
}

// Delete 删除角色记录。
func (m *RoleModel) Delete(ctx context.Context, id uint64) error {
	return m.DB(ctx).Where("id = ?", id).Delete(&RoleEntity{}).Error
}

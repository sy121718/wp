package adminmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const tableNameSysPermission = "sys_permission"

const (
	PermissionStatusDisabled = 0 // 禁用
	PermissionStatusEnabled  = 1 // 启用
)

// PermissionEntity 对应 sys_permission 表字段（权限点目录，非 Casbin 授权记录）。
type PermissionEntity struct {
	ID             uint64     `gorm:"column:id;primaryKey"`                                 // 主键ID
	PermissionCode string     `gorm:"column:permission_code;type:varchar(100);uniqueIndex"` // 权限编码，如 admin:list
	PermissionName string     `gorm:"column:permission_name;type:varchar(100)"`             // 权限名称，如 管理员列表
	Module         string     `gorm:"column:module;type:varchar(50);index"`                 // 所属模块，如 admin
	APIPath        string     `gorm:"column:api_path;type:varchar(200)"`                    // 后端接口路径
	APIMethod      string     `gorm:"column:api_method;type:varchar(10);default:GET"`       // 请求方法 GET/POST
	Status         int        `gorm:"column:status;type:tinyint;default:1;index"`           // 状态：0=禁用 1=启用
	Remark         *string    `gorm:"column:remark;type:varchar(200)"`                      // 备注
	CreateBy       uint64     `gorm:"column:create_by;type:bigint unsigned"`                // 创建人ID
	CreateTime     *time.Time `gorm:"column:create_time;type:datetime(3)"`                  // 创建时间
	UpdateBy       uint64     `gorm:"column:update_by;type:bigint unsigned"`                // 更新人ID
	UpdateTime     *time.Time `gorm:"column:update_time;type:datetime(3)"`                  // 更新时间
}

// PermissionModel 权限点数据访问，持有 gorm 连接。
type PermissionModel struct {
	db *gorm.DB
}

// NewPermissionModel 外部传入 db。
func NewPermissionModel(db *gorm.DB) *PermissionModel {
	return &PermissionModel{db: db}
}

// DB 返回绑定当前实体的数据库入口，支持链式调用。
func (m *PermissionModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PermissionEntity{})
}

// GetByID 根据 ID 查询权限点，不存在返回 nil。
func (m *PermissionModel) GetByID(ctx context.Context, id uint64) (*PermissionEntity, error) {
	var entity PermissionEntity
	err := m.DB(ctx).Where("id = ?", id).First(&entity).Error
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

// TableName 指定表名。
func (PermissionEntity) TableName() string {
	return tableNameSysPermission
}

// BeforeCreate 创建前 hook：补齐时间。
func (e *PermissionEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	e.CreateTime = &now
	e.UpdateTime = &now
	return nil
}

// BeforeUpdate 更新前 hook：刷新更新时间。
func (e *PermissionEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	e.UpdateTime = &now
	return nil
}

// GetByCode 根据权限编码查询，不存在返回 nil。
func (m *PermissionModel) GetByCode(ctx context.Context, code string) (*PermissionEntity, error) {
	var entity PermissionEntity
	err := m.DB(ctx).Where("permission_code = ?", code).First(&entity).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListByModule 按模块查询已启用的权限点列表。
func (m *PermissionModel) ListByModule(ctx context.Context, module string) ([]PermissionEntity, error) {
	var list []PermissionEntity
	err := m.DB(ctx).Where("module = ? AND status = ?", module, PermissionStatusEnabled).
		Order("id ASC").Find(&list).Error
	return list, err
}

// ListEnabled 查询所有已启用的权限点（用于 Casbin 策略构建）。
func (m *PermissionModel) IsEnabledAll(ctx context.Context) ([]PermissionEntity, error) {
	var list []PermissionEntity
	err := m.DB(ctx).Where("status = ?", PermissionStatusEnabled).
		Order("id ASC").Find(&list).Error
	return list, err
}

// ListByIDs 按 ID 列表批量查询。
func (m *PermissionModel) ListByIDs(ctx context.Context, ids []uint64) ([]PermissionEntity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []PermissionEntity
	err := m.DB(ctx).Where("id IN ?", ids).Order("id ASC").Find(&list).Error
	return list, err
}

// ListByCodes 按 permission_code 列表批量查询。
func (m *PermissionModel) ListByCodes(ctx context.Context, codes []string) ([]PermissionEntity, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	var list []PermissionEntity
	err := m.DB(ctx).Where("permission_code IN ?", codes).Order("id ASC").Find(&list).Error
	return list, err
}

// ExistsEnabledCode 检查给定的 code 是否存在且启用。
func (m *PermissionModel) ExistsEnabledCode(ctx context.Context, code string) (bool, error) {
	var count int64
	err := m.DB(ctx).Where("permission_code = ? AND status = ?", code, PermissionStatusEnabled).Count(&count).Error
	return count > 0, err
}

// Create 新建权限点。
func (m *PermissionModel) Create(ctx context.Context, e *PermissionEntity) error {
	return m.DB(ctx).Create(e).Error
}

// Update 全量更新权限点。
func (m *PermissionModel) Update(ctx context.Context, e *PermissionEntity) error {
	return m.DB(ctx).Where("id = ?", e.ID).Updates(e).Error
}

// DeleteByIDs 批量删除权限点。
func (m *PermissionModel) DeleteByIDs(ctx context.Context, ids []uint64) (int64, error) {
	result := m.DB(ctx).Where("id IN ?", ids).Delete(&PermissionEntity{})
	return result.RowsAffected, result.Error
}

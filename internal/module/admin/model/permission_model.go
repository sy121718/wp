package adminmodel

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNameSysPermission = "sys_permission"

const (
	PermissionStatusDisabled = 0 // 禁用
	PermissionStatusEnabled  = 1 // 启用
)

// PermissionEntity 对应 sys_permission 表字段（权限点目录，非 Casbin 授权记录）。
type PermissionEntity struct {
	ID             uint64     `gorm:"column:id;primaryKey"`               // 主键ID
	PermissionCode string     `gorm:"column:permission_code;uniqueIndex"` // 权限编码，如 admin:list
	PermissionName string     `gorm:"column:permission_name"`             // 权限名称，如 管理员列表
	Module         string     `gorm:"column:module;index"`                // 所属模块，如 admin
	APIPath        string     `gorm:"column:api_path"`                    // 后端接口路径
	APIMethod      string     `gorm:"column:api_method;default:GET"`      // 请求方法 GET/POST
	Status         int        `gorm:"column:status;index"`                // 状态：0=禁用 1=启用；不带 default tag，避免 gorm 把显式 0（禁用）改写为 1（见 SysRuleEntity.Status 注释）
	Remark         *string    `gorm:"column:remark"`                      // 备注
	CreateBy       uint64     `gorm:"column:create_by"`                   // 创建人ID
	CreateTime     *time.Time `gorm:"column:create_time"`                 // 创建时间
	UpdateBy       uint64     `gorm:"column:update_by"`                   // 更新人ID
	UpdateTime     *time.Time `gorm:"column:update_time"`                 // 更新时间
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

// permissionUpdateColumns UpdateTx 专用的显式列集合（含 status 零值）。
var permissionUpdateColumns = []string{
	"permission_code", "permission_name", "module", "api_path", "api_method",
	"status", "remark", "update_by", "update_time",
}


// Transaction 透传事务：一次写操作里「权限点行 + Casbin 策略行」两处持久化写
// 必须同事务，边界由 service 决定（见 AGENTS.md「写操作的事务与回滚」）。
func (m *PermissionModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// UpdateTx 在调用方事务内更新权限点（**唯一的写入口** —— 非事务版 Update 已删除：
// 权限点变更必然与 Casbin 策略行同事务，留下裸句柄版本只会让人写错）。
// 显式 Select permissionUpdateColumns（含 status 零值）：否则 Updates(struct) 跳过零值字段，
// 显式禁用（status=0）不落库，权限点保持启用。
func (m *PermissionModel) UpdateTx(ctx context.Context, tx *gorm.DB, e *PermissionEntity) error {
	return tx.WithContext(ctx).Model(&PermissionEntity{}).Where("id = ?", e.ID).
		Select(permissionUpdateColumns).Updates(e).Error
}

// LockByIDTx 在调用方事务内按主键加行锁读取（SELECT ... FOR UPDATE）。
// 读-改-写路径必须用这个：先读出旧定义、再算新定义、再写回，不加锁就会与并发更新互相覆盖。
// 记录不存在返回 nil, nil（调用方判空后给出业务错误，不把 gorm 的内部错误透出去）。
func (m *PermissionModel) LockByIDTx(ctx context.Context, tx *gorm.DB, id uint64) (*PermissionEntity, error) {
	var entity PermissionEntity
	err := tx.WithContext(ctx).Model(&PermissionEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// DeleteByIDs 批量删除权限点。
func (m *PermissionModel) DeleteByIDs(ctx context.Context, ids []uint64) (int64, error) {
	result := m.DB(ctx).Where("id IN ?", ids).Delete(&PermissionEntity{})
	return result.RowsAffected, result.Error
}

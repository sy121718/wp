// Package adminmodel 合并后的统一模型包。
package adminmodel

import (
	"context"
	"errors"
	"time"

	"go_wp/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNameSysRole = "sys_role"

const (
	RoleStatusDisabled = 0
	RoleStatusEnabled  = 1
)

// RoleEntity 对应 sys_role 表。
type RoleEntity struct {
	ID       uint64 `gorm:"column:id;primaryKey"`
	RoleCode string `gorm:"column:role_code;uniqueIndex"`
	RoleName string `gorm:"column:role_name"`
	// Status 不带 gorm default tag：gorm 对带 default 的字段在零值时会用 DB 默认值
	// 替换并回写 struct，导致显式传入的 status=0（禁用）被改写成 1（启用）。
	// service 层总是显式设置 Status，DB 列 DEFAULT 1 仅兜底直接 SQL 插入。
	Status     int        `gorm:"column:status"`
	IsSystem   int        `gorm:"column:is_system;default:0"`
	SortOrder  int        `gorm:"column:sort_order;default:0"`
	Remark     *string    `gorm:"column:remark"`
	CreateBy   uint64     `gorm:"column:create_by"`
	CreateTime *time.Time `gorm:"column:create_time"`
	UpdateBy   uint64     `gorm:"column:update_by"`
	UpdateTime *time.Time `gorm:"column:update_time"`
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
		escaped := "%" + database.EscapeLikePattern(keyword) + "%"
		query = query.Where("role_code LIKE ? ESCAPE '\\' OR role_name LIKE ? ESCAPE '\\'", escaped, escaped)
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


// Transaction 透传事务：一次写操作里「角色行 + Casbin g2 启用标记」两处持久化写
// 必须同事务，边界由 service 决定。
func (m *RoleModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// CreateTx 在调用方事务内新建角色（**唯一的写入口** —— 非事务版 Create 已删除：
// 角色的新建必然与 Casbin g2 策略行同事务，留下裸句柄版本只会让人写错）。
// Status 已无 default tag（见字段注释），零值 0（禁用）可直接落库，无需显式 Select 列。
func (m *RoleModel) CreateTx(ctx context.Context, tx *gorm.DB, e *RoleEntity) error {
	return tx.WithContext(ctx).Model(&RoleEntity{}).Create(e).Error
}

// LockByIDTx 在调用方事务内按主键加行锁读取（SELECT ... FOR UPDATE）。
// 启停角色是读-改-写（先读旧 status 判断要不要动 g2），必须加锁，否则并发启停会互相覆盖。
// 记录不存在返回 nil, nil。
func (m *RoleModel) LockByIDTx(ctx context.Context, tx *gorm.DB, id uint64) (*RoleEntity, error) {
	var entity RoleEntity
	err := tx.WithContext(ctx).Model(&RoleEntity{}).
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


// roleUpdateColumns UpdateTx 专用的显式列集合（含 status 零值）。
var roleUpdateColumns = []string{"role_name", "status", "sort_order", "remark", "update_by", "update_time"}

// UpdateTx 在调用方事务内更新角色元信息（**唯一的写入口** —— 非事务版 Update 已删除）。
// 显式 Select roleUpdateColumns（含 status 零值），否则 gorm 结构体更新会跳过零值字段，
// 导致停用角色（status=0）落库失败、重新启用时状态比对失真。
// 注意：Model(&RoleEntity{}) + Updates(e) 时 gorm 的 BeforeUpdate hook 在 Model 的
// 空实例上触发，修改不会进入 SET 子句（见 gorm callbacks.SetupUpdateReflectValue），
// 因此这里显式刷新 e.UpdateTime；update_by 保留调用方传入值（service 层未传时
// 沿用实体原值），与 SysRule/Permission 的 Update 列模式一致。
func (m *RoleModel) UpdateTx(ctx context.Context, tx *gorm.DB, e *RoleEntity) error {
	now := time.Now()
	e.UpdateTime = &now
	return tx.WithContext(ctx).Model(&RoleEntity{}).Where("id = ?", e.ID).
		Select(roleUpdateColumns).Updates(e).Error
}

// Delete 删除角色记录。
func (m *RoleModel) Delete(ctx context.Context, id uint64) error {
	return m.DB(ctx).Where("id = ?", id).Delete(&RoleEntity{}).Error
}

// DeleteTx 在调用方事务内删除角色记录（语义与 Delete 一致）。
//
// 删角色同时要清掉 sys_casbin_rule 里该角色的全部策略行，两处写必须同事务，
// 边界由 service 决定（见 RoleDelete 的注释）。
func (m *RoleModel) DeleteTx(ctx context.Context, tx *gorm.DB, id uint64) error {
	return tx.WithContext(ctx).Model(&RoleEntity{}).Where("id = ?", id).Delete(&RoleEntity{}).Error
}

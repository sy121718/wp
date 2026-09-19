// Package adminmodel 合并后的统一模型包。
// 封装 sys_dept 表的 CRUD 操作，提供树形结构维护所需的查询能力。
package adminmodel

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNameSysDept = "sys_dept"

const (
	DeptStatusDisabled = 0
	DeptStatusEnabled  = 1
)

// DeptEntity 对应 sys_dept 表。
type DeptEntity struct {
	ID        uint64  `gorm:"column:id;primaryKey"`
	ParentID  uint64  `gorm:"column:parent_id;default:0"`
	Ancestors string  `gorm:"column:ancestors"`
	DeptName  string  `gorm:"column:dept_name"`
	DeptCode  string  `gorm:"column:dept_code;uniqueIndex"`
	LeaderID  *uint64 `gorm:"column:leader_id"`
	SortOrder int     `gorm:"column:sort_order;default:0"`
	// Status 不带 gorm default tag：避免 gorm 把显式 0（禁用）改写为 1（见 SysRuleEntity.Status 注释）。
	Status     int        `gorm:"column:status"`
	Remark     *string    `gorm:"column:remark"`
	CreateBy   uint64     `gorm:"column:create_by"`
	CreateTime *time.Time `gorm:"column:create_time"`
	UpdateBy   uint64     `gorm:"column:update_by"`
	UpdateTime *time.Time `gorm:"column:update_time"`
}

func (DeptEntity) TableName() string {
	return tableNameSysDept
}

// DeptModel 部门数据访问对象，封装 sys_dept 表的查询与写入操作。
type DeptModel struct {
	db *gorm.DB
}

// NewDeptModel 创建部门模型。
// db 为 GORM 数据库连接实例。
func NewDeptModel(db *gorm.DB) *DeptModel {
	return &DeptModel{db: db}
}

// DB 返回绑定 DeptEntity 的 GORM DB 实例，包含上下文和预置模型。
func (m *DeptModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&DeptEntity{})
}

func (e *DeptEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	e.CreateTime = &now
	e.UpdateTime = &now
	return nil
}

func (e *DeptEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	e.UpdateTime = &now
	return nil
}

// GetByID 根据主键 ID 查询单个部门。记录不存在时返回 nil, nil。
func (m *DeptModel) GetByID(ctx context.Context, id uint64) (*DeptEntity, error) {
	var entity DeptEntity
	err := m.DB(ctx).Where("id = ?", id).First(&entity).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetByCode 根据部门编码（dept_code）查询部门。记录不存在时返回 nil, nil。
func (m *DeptModel) GetByCode(ctx context.Context, code string) (*DeptEntity, error) {
	var entity DeptEntity
	err := m.DB(ctx).Where("dept_code = ?", code).First(&entity).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetByCodeTx 在调用方事务内按部门编码查询，不存在返回 nil, nil。
func (m *DeptModel) GetByCodeTx(ctx context.Context, tx *gorm.DB, code string) (*DeptEntity, error) {
	var entity DeptEntity
	err := tx.WithContext(ctx).Model(&DeptEntity{}).Where("dept_code = ?", code).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListAll 查询全部部门，按 sort_order、id 升序排列。
func (m *DeptModel) ListAll(ctx context.Context) ([]DeptEntity, error) {
	var list []DeptEntity
	err := m.DB(ctx).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

// ListByAncestors 查询 ancestors 字段以指定前缀开头的所有子孙部门。
func (m *DeptModel) ListByAncestors(ctx context.Context, ancestorPrefix string) ([]DeptEntity, error) {
	var list []DeptEntity
	// 查询 ancestors 以 ancestorPrefix 开头的记录（子孙节点）
	err := m.DB(ctx).Where("ancestors LIKE ?", ancestorPrefix+"%").Find(&list).Error
	return list, err
}

// CountByParentID 统计指定父部门下的直接子部门数量。
func (m *DeptModel) CountByParentID(ctx context.Context, parentID uint64) (int64, error) {
	var count int64
	err := m.DB(ctx).Where("parent_id = ?", parentID).Count(&count).Error
	return count, err
}

// Create 新增一条部门记录。
func (m *DeptModel) Create(ctx context.Context, e *DeptEntity) error {
	return m.DB(ctx).Create(e).Error
}

// deptUpdateColumns UpdateTx 专用的显式列集合（含 status 零值）。
var deptUpdateColumns = []string{
	"parent_id", "ancestors", "dept_name", "dept_code", "leader_id",
	"sort_order", "status", "remark",
}

// Transaction 透传事务：移动部门节点时「自身行 + 全部子孙行」两处持久化写必须同事务
// （子孙 ancestors 更新失败会让 pkg/datarule 的部门范围匹配错乱），边界由 service 决定。
func (m *DeptModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// GetByIDTx 在调用方事务内按主键查询部门（不加锁），不存在返回 nil, nil。
func (m *DeptModel) GetByIDTx(ctx context.Context, tx *gorm.DB, id uint64) (*DeptEntity, error) {
	var entity DeptEntity
	err := tx.WithContext(ctx).Model(&DeptEntity{}).Where("id = ?", id).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// LockByIDTx 在调用方事务内按主键加行锁读取（SELECT ... FOR UPDATE）。
// 移动节点是读-改-写（读旧 ancestors 算子孙前缀、写自身、再批量改子孙）：被移动的行
// 必须锁住，否则并发移动同一子树会按各自的旧快照算前缀，把子孙链改错。
func (m *DeptModel) LockByIDTx(ctx context.Context, tx *gorm.DB, id uint64) (*DeptEntity, error) {
	var entity DeptEntity
	err := tx.WithContext(ctx).Model(&DeptEntity{}).
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

// UpdateTx 在调用方事务内按主键更新部门记录（**唯一的写入口** —— 非事务版 Update 已删除：
// 部门移动必然与子孙 ancestors 批量更新同事务）。
// 显式 Select deptUpdateColumns，避免 Updates 对零值（如 status=0 禁用）不落库。
func (m *DeptModel) UpdateTx(ctx context.Context, tx *gorm.DB, e *DeptEntity) error {
	return tx.WithContext(ctx).Model(&DeptEntity{}).Where("id = ?", e.ID).
		Select(deptUpdateColumns).Updates(e).Error
}

// UpdateAncestorsTx 在调用方事务内批量更新子孙 ancestors（**唯一的写入口** ——
// 非事务版 UpdateAncestors 已删除：它只可能产出「自身改了、子孙没跟上」的半截状态）。
// 与「自身 UpdateTx」必须同事务：只改了自身 ancestors 而子孙没跟上，会让 SELF_AND_CHILDREN
// 数据范围匹配到错误的部门集合（多看到 / 少看到别的部门数据），且不会报错。
//
// 逗号边界匹配：直属子 ancestors = oldPrefix，更深层以 oldPrefix+"," 开头。
// 旧实现用 `LIKE oldPrefix%` 会把 ID 前缀重叠的无关部门卷进来
// （如移动部门 2 时误匹配部门 21 的 ancestors "0,20"），损坏部门树。
func (m *DeptModel) UpdateAncestorsTx(ctx context.Context, tx *gorm.DB, oldPrefix, newPrefix string) error {
	return tx.WithContext(ctx).Model(&DeptEntity{}).
		Where("ancestors = ? OR ancestors LIKE ?", oldPrefix, oldPrefix+",%").
		Update("ancestors", gorm.Expr("REPLACE(ancestors, ?, ?)", oldPrefix, newPrefix)).Error
}

// Delete 根据主键软删除部门记录。
func (m *DeptModel) Delete(ctx context.Context, id uint64) error {
	return m.DB(ctx).Where("id = ?", id).Delete(&DeptEntity{}).Error
}

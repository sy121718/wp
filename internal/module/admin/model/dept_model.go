// Package adminmodel 合并后的统一模型包。
// 封装 sys_dept 表的 CRUD 操作，提供树形结构维护所需的查询能力。
package adminmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const tableNameSysDept = "sys_dept"

const (
	DeptStatusDisabled = 0
	DeptStatusEnabled  = 1
)

// DeptEntity 对应 sys_dept 表。
type DeptEntity struct {
	ID         uint64     `gorm:"column:id;primaryKey"`
	ParentID   uint64     `gorm:"column:parent_id;default:0"`
	Ancestors  string     `gorm:"column:ancestors;type:varchar(500)"`
	DeptName   string     `gorm:"column:dept_name;type:varchar(100)"`
	DeptCode   string     `gorm:"column:dept_code;type:varchar(50);uniqueIndex"`
	LeaderID   *uint64    `gorm:"column:leader_id"`
	SortOrder  int        `gorm:"column:sort_order;default:0"`
	Status     int        `gorm:"column:status;default:1"`
	Remark     *string    `gorm:"column:remark;type:varchar(200)"`
	CreateBy   uint64     `gorm:"column:create_by;type:bigint unsigned"`
	CreateTime *time.Time `gorm:"column:create_time;type:datetime(3)"`
	UpdateBy   uint64     `gorm:"column:update_by;type:bigint unsigned"`
	UpdateTime *time.Time `gorm:"column:update_time;type:datetime(3)"`
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

// Update 按主键更新部门记录。
// 显式 Select 全字段，避免 Updates 对零值（如 status=0 禁用）不落库。
func (m *DeptModel) Update(ctx context.Context, e *DeptEntity) error {
	return m.DB(ctx).Where("id = ?", e.ID).
		Select("parent_id", "ancestors", "dept_name", "dept_code", "leader_id", "sort_order", "status", "remark").
		Updates(e).Error
}

// UpdateAncestors 批量更新子孙节点的 ancestors 前缀（移动部门时使用）。
// 将 oldPrefix 开头的 ancestors 替换为 newPrefix。
//
// 逗号边界匹配：直属子 ancestors = oldPrefix，更深层以 oldPrefix+"," 开头。
// 旧实现用 `LIKE oldPrefix%` 会把 ID 前缀重叠的无关部门卷进来
// （如移动部门 2 时误匹配部门 21 的 ancestors "0,20"），损坏部门树。
func (m *DeptModel) UpdateAncestors(ctx context.Context, oldPrefix, newPrefix string) error {
	return m.DB(ctx).Model(&DeptEntity{}).
		Where("ancestors = ? OR ancestors LIKE ?", oldPrefix, oldPrefix+",%").
		Update("ancestors", gorm.Expr("REPLACE(ancestors, ?, ?)", oldPrefix, newPrefix)).Error
}

// Delete 根据主键软删除部门记录。
func (m *DeptModel) Delete(ctx context.Context, id uint64) error {
	return m.DB(ctx).Where("id = ?", id).Delete(&DeptEntity{}).Error
}

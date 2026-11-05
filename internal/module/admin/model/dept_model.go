package adminmodel

import (
	"context"
	"errors"
	"time"

	"go_wp/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNameSysDept = "sys_dept"

const (
	DeptStatusDisabled = 0
	DeptStatusEnabled  = 1
)

type DeptEntity struct {
	ID        uint64  `gorm:"column:id;primaryKey"`
	ParentID  uint64  `gorm:"column:parent_id;default:0"`
	Ancestors string  `gorm:"column:ancestors"`
	DeptName  string  `gorm:"column:dept_name"`
	DeptCode  string  `gorm:"column:dept_code;uniqueIndex"`
	LeaderID  *uint64 `gorm:"column:leader_id"`
	SortOrder int     `gorm:"column:sort_order;default:0"`

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

type DeptModel struct {
	db *gorm.DB
}

func NewDeptModel(db *gorm.DB) *DeptModel {
	return &DeptModel{db: db}
}

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

func (m *DeptModel) ListAll(ctx context.Context) ([]DeptEntity, error) {
	var list []DeptEntity
	err := m.DB(ctx).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

type DeptParentOption struct {
	ID        uint64
	ParentID  uint64
	DeptName  string
	SortOrder int
}

func (m *DeptModel) ListPage(ctx context.Context, page, limit int, keyword string) (int64, []DeptEntity, error) {
	query := m.DB(ctx)
	if keyword != "" {
		pattern := "%" + database.EscapeLikePattern(keyword) + "%"
		query = query.Where("dept_name LIKE ? ESCAPE '\\' OR dept_code LIKE ? ESCAPE '\\' OR remark LIKE ? ESCAPE '\\'", pattern, pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, nil, err
	}
	var rows []DeptEntity
	if total > 0 && int64(page-1) > (total-1)/int64(limit) {
		page = int((total-1)/int64(limit)) + 1
	}
	err := query.Order("sort_order ASC, id ASC").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error
	return total, rows, err
}

func (m *DeptModel) ListParentOptions(ctx context.Context) ([]DeptParentOption, error) {
	var options []DeptParentOption
	err := m.DB(ctx).Select("id, parent_id, dept_name, sort_order").
		Order("sort_order ASC, id ASC").Find(&options).Error
	return options, err
}

func (m *DeptModel) ListByAncestors(ctx context.Context, ancestorPrefix string) ([]DeptEntity, error) {
	var list []DeptEntity

	err := m.DB(ctx).Where("ancestors LIKE ?", ancestorPrefix+"%").Find(&list).Error
	return list, err
}

func (m *DeptModel) CountByParentID(ctx context.Context, parentID uint64) (int64, error) {
	var count int64
	err := m.DB(ctx).Where("parent_id = ?", parentID).Count(&count).Error
	return count, err
}

func (m *DeptModel) Create(ctx context.Context, e *DeptEntity) error {
	return m.DB(ctx).Create(e).Error
}

// CreateTx 在调用方事务内建部门。
//
// 调用方要在这同一个事务里锁父行、读父链，插入不能换连接，
// 否则父链快照与插入不在同一份一致性视图里。
func (m *DeptModel) CreateTx(ctx context.Context, tx *gorm.DB, e *DeptEntity) error {
	return tx.WithContext(ctx).Create(e).Error
}

var deptUpdateColumns = []string{
	"parent_id", "ancestors", "dept_name", "dept_code", "leader_id",
	"sort_order", "status", "remark",
}

func (m *DeptModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// LockTreeTx 统一先按主键锁定部门树，结构写入随后读取最新父链。
func (m *DeptModel) LockTreeTx(ctx context.Context, tx *gorm.DB) error {
	var rows []DeptEntity
	return tx.WithContext(ctx).Model(&DeptEntity{}).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id ASC").Find(&rows).Error
}

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

func (m *DeptModel) UpdateTx(ctx context.Context, tx *gorm.DB, e *DeptEntity) error {
	return tx.WithContext(ctx).Model(&DeptEntity{}).Where("id = ?", e.ID).
		Select(deptUpdateColumns).Updates(e).Error
}

func (m *DeptModel) UpdateAncestorsTx(ctx context.Context, tx *gorm.DB, oldPrefix, newPrefix string) error {
	return tx.WithContext(ctx).Model(&DeptEntity{}).
		Where("ancestors = ? OR ancestors LIKE ?", oldPrefix, oldPrefix+",%").
		Update("ancestors", gorm.Expr("REPLACE(ancestors, ?, ?)", oldPrefix, newPrefix)).Error
}

func (m *DeptModel) Delete(ctx context.Context, id uint64) error {
	return m.DB(ctx).Where("id = ?", id).Delete(&DeptEntity{}).Error
}

// CountByParentIDTx 在调用方事务内数子部门。
// 删除端必须用事务版：非事务版读的是另一条连接，看不到本事务的行锁效果。
func (m *DeptModel) CountByParentIDTx(ctx context.Context, tx *gorm.DB, parentID uint64) (int64, error) {
	var count int64
	err := tx.WithContext(ctx).Model(&DeptEntity{}).Where("parent_id = ?", parentID).Count(&count).Error
	return count, err
}

// DeleteTx 在调用方事务内删部门。
func (m *DeptModel) DeleteTx(ctx context.Context, tx *gorm.DB, id uint64) error {
	return tx.WithContext(ctx).Model(&DeptEntity{}).Where("id = ?", id).Delete(&DeptEntity{}).Error
}

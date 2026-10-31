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

type RoleEntity struct {
	ID       uint64 `gorm:"column:id;primaryKey"`
	RoleCode string `gorm:"column:role_code;uniqueIndex"`
	RoleName string `gorm:"column:role_name"`

	Status     int        `gorm:"column:status"`
	IsSystem   int        `gorm:"column:is_system;default:0"`
	SortOrder  int        `gorm:"column:sort_order;default:0"`
	Remark     *string    `gorm:"column:remark"`
	CreateBy   uint64     `gorm:"column:create_by"`
	CreateTime *time.Time `gorm:"column:create_time"`
	UpdateBy   uint64     `gorm:"column:update_by"`
	UpdateTime *time.Time `gorm:"column:update_time"`
}

func (RoleEntity) TableName() string {
	return tableNameSysRole
}

type RoleModel struct {
	db *gorm.DB
}

func NewRoleModel(db *gorm.DB) *RoleModel {
	return &RoleModel{db: db}
}

func (m *RoleModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&RoleEntity{})
}

func (e *RoleEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	e.CreateTime = &now
	e.UpdateTime = &now
	return nil
}

func (e *RoleEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	e.UpdateTime = &now
	return nil
}

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

func (m *RoleModel) ListByIDs(ctx context.Context, ids []uint64) ([]RoleEntity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []RoleEntity
	err := m.DB(ctx).Where("id IN ?", ids).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

func (m *RoleModel) ListByCodes(ctx context.Context, codes []string) ([]RoleEntity, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	var list []RoleEntity
	err := m.DB(ctx).Where("role_code IN ?", codes).Find(&list).Error
	return list, err
}

func (m *RoleModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *RoleModel) CreateTx(ctx context.Context, tx *gorm.DB, e *RoleEntity) error {
	return tx.WithContext(ctx).Model(&RoleEntity{}).Create(e).Error
}

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

var roleUpdateColumns = []string{"role_name", "status", "sort_order", "remark", "update_by", "update_time"}

func (m *RoleModel) UpdateTx(ctx context.Context, tx *gorm.DB, e *RoleEntity) error {
	now := time.Now()
	e.UpdateTime = &now
	return tx.WithContext(ctx).Model(&RoleEntity{}).Where("id = ?", e.ID).
		Select(roleUpdateColumns).Updates(e).Error
}

func (m *RoleModel) Delete(ctx context.Context, id uint64) error {
	return m.DB(ctx).Where("id = ?", id).Delete(&RoleEntity{}).Error
}

func (m *RoleModel) DeleteTx(ctx context.Context, tx *gorm.DB, id uint64) error {
	return tx.WithContext(ctx).Model(&RoleEntity{}).Where("id = ?", id).Delete(&RoleEntity{}).Error
}

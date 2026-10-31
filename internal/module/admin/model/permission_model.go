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
	PermissionStatusDisabled = 0
	PermissionStatusEnabled  = 1
)

type PermissionEntity struct {
	ID             uint64     `gorm:"column:id;primaryKey"`
	PermissionCode string     `gorm:"column:permission_code;uniqueIndex"`
	PermissionName string     `gorm:"column:permission_name"`
	Module         string     `gorm:"column:module;index"`
	APIPath        string     `gorm:"column:api_path"`
	APIMethod      string     `gorm:"column:api_method;default:GET"`
	Status         int        `gorm:"column:status;index"`
	Remark         *string    `gorm:"column:remark"`
	CreateBy       uint64     `gorm:"column:create_by"`
	CreateTime     *time.Time `gorm:"column:create_time"`
	UpdateBy       uint64     `gorm:"column:update_by"`
	UpdateTime     *time.Time `gorm:"column:update_time"`
}

type PermissionModel struct {
	db *gorm.DB
}

func NewPermissionModel(db *gorm.DB) *PermissionModel {
	return &PermissionModel{db: db}
}

func (m *PermissionModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PermissionEntity{})
}

func (m *PermissionModel) GetByID(ctx context.Context, id uint64) (*PermissionEntity, error) {
	var entity PermissionEntity
	err := m.DB(ctx).Where("id = ?", id).First(&entity).Error
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

func (PermissionEntity) TableName() string {
	return tableNameSysPermission
}

func (e *PermissionEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	e.CreateTime = &now
	e.UpdateTime = &now
	return nil
}

func (e *PermissionEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	e.UpdateTime = &now
	return nil
}

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

func (m *PermissionModel) ListByModule(ctx context.Context, module string) ([]PermissionEntity, error) {
	var list []PermissionEntity
	err := m.DB(ctx).Where("module = ? AND status = ?", module, PermissionStatusEnabled).
		Order("id ASC").Find(&list).Error
	return list, err
}

func (m *PermissionModel) IsEnabledAll(ctx context.Context) ([]PermissionEntity, error) {
	var list []PermissionEntity
	err := m.DB(ctx).Where("status = ?", PermissionStatusEnabled).
		Order("id ASC").Find(&list).Error
	return list, err
}

func (m *PermissionModel) ListByIDs(ctx context.Context, ids []uint64) ([]PermissionEntity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []PermissionEntity
	err := m.DB(ctx).Where("id IN ?", ids).Order("id ASC").Find(&list).Error
	return list, err
}

func (m *PermissionModel) ListByCodes(ctx context.Context, codes []string) ([]PermissionEntity, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	var list []PermissionEntity
	err := m.DB(ctx).Where("permission_code IN ?", codes).Order("id ASC").Find(&list).Error
	return list, err
}

func (m *PermissionModel) ExistsEnabledCode(ctx context.Context, code string) (bool, error) {
	var count int64
	err := m.DB(ctx).Where("permission_code = ? AND status = ?", code, PermissionStatusEnabled).Count(&count).Error
	return count > 0, err
}

func (m *PermissionModel) Create(ctx context.Context, e *PermissionEntity) error {
	return m.DB(ctx).Create(e).Error
}

var permissionUpdateColumns = []string{
	"permission_code", "permission_name", "module", "api_path", "api_method",
	"status", "remark", "update_by", "update_time",
}

func (m *PermissionModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *PermissionModel) UpdateTx(ctx context.Context, tx *gorm.DB, e *PermissionEntity) error {
	return tx.WithContext(ctx).Model(&PermissionEntity{}).Where("id = ?", e.ID).
		Select(permissionUpdateColumns).Updates(e).Error
}

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

func (m *PermissionModel) DeleteByIDs(ctx context.Context, ids []uint64) (int64, error) {
	result := m.DB(ctx).Where("id IN ?", ids).Delete(&PermissionEntity{})
	return result.RowsAffected, result.Error
}

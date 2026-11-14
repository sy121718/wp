package adminmodel

import (
	"context"
	"errors"
	"strings"
	"time"

	"go_wp/pkg/database"

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

// ListByPathMethod 返回 api_path/api_method 命中的目录行（含被停用的）。
//
// 页面与后台写入口复用别的操作权限时不带自己的 Perm，核对它们只能按
// 「实际检查的 AuthObject/AuthMethod」去目录里找对应策略行。
func (m *PermissionModel) ListByPathMethod(ctx context.Context, path, method string) ([]PermissionEntity, error) {
	var list []PermissionEntity
	err := m.DB(ctx).Where("api_path = ? AND upper(api_method) = ?", path, strings.ToUpper(method)).
		Order("id ASC").Find(&list).Error
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

// ListByIDsTx 在调用方事务内按 id 取权限点，供批量删除在同一事务里先校验再删。
func (m *PermissionModel) ListByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) ([]PermissionEntity, error) {
	var list []PermissionEntity
	if len(ids) == 0 {
		return list, nil
	}
	err := tx.WithContext(ctx).Where("id IN ?", ids).Find(&list).Error
	return list, err
}

// LockByIDsTx 在调用方事务内按 id 升序锁住这批权限点行。
//
// 授予端要锁的实体是 sys_permission（权限码属于它），不是 sys_menus：
// 菜单只是把权限码聚起来的载体，真正被 casbin 引用的是权限码对应的这一行。
func (m *PermissionModel) LockByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) ([]PermissionEntity, error) {
	var list []PermissionEntity
	if len(ids) == 0 {
		return list, nil
	}
	err := tx.WithContext(ctx).Model(&PermissionEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", ids).Order("id ASC").Find(&list).Error
	return list, err
}

// LockByCodesTx 在调用方事务内锁住这批权限码对应的权限点行（授予端的主路径：
// 前端交的是菜单 id，真正要锁的是这些菜单背后的权限码）。
//
// ORDER BY id 而不是 permission_code：加锁顺序由这条 SQL 决定，事后排序改不了已经
// 取得的锁序。删除端（PermissionsBulkDelete）按 id 升序锁同一批行，两端因此同序；
// 用 WHERE permission_code IN 只是筛行，不参与锁序。
func (m *PermissionModel) LockByCodesTx(ctx context.Context, tx *gorm.DB, codes []string) ([]PermissionEntity, error) {
	var list []PermissionEntity
	if len(codes) == 0 {
		return list, nil
	}
	err := tx.WithContext(ctx).Model(&PermissionEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("permission_code IN ?", codes).Order("id ASC").Find(&list).Error
	return list, err
}

// DeleteByIDsTx 在调用方事务内批量删除，返回实际删除行数。
func (m *PermissionModel) DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := tx.WithContext(ctx).Where("id IN ?", ids).Delete(&PermissionEntity{})
	return result.RowsAffected, result.Error
}

// PermListFilter 是权限点列表的筛选条件。
type PermListFilter struct {
	Module  string
	Code    string
	APIPath string
	Status  *int
	Page    int
	Limit   int
}

// ListPage 分页查权限点，返回总数与本页行。
func (m *PermissionModel) ListPage(ctx context.Context, f PermListFilter) (int64, []PermissionEntity, error) {
	query := m.DB(ctx)
	if f.Module != "" {
		query = query.Where("module = ?", f.Module)
	}
	if f.Code != "" {
		query = query.Where("permission_code LIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(f.Code)+"%")
	}
	if f.APIPath != "" {
		query = query.Where("api_path LIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(f.APIPath)+"%")
	}
	if f.Status != nil {
		query = query.Where("status = ?", *f.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, nil, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 1
	}
	page := f.Page
	if page <= 0 {
		page = 1
	}
	var list []PermissionEntity
	err := query.Order("id DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return total, list, err
}

// DetailByID 读权限点详情列，不存在时返回 nil。
func (m *PermissionModel) DetailByID(ctx context.Context, id uint64) (*PermissionEntity, error) {
	var entity PermissionEntity
	err := m.DB(ctx).
		Select("id", "permission_code", "permission_name", "module", "api_path", "api_method",
			"status", "remark", "create_time", "update_time").
		Where("id = ?", id).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListEnabledForOptions 列出可选权限点（启用状态），可按模块收窄，供勾选面板用。
func (m *PermissionModel) ListEnabledForOptions(ctx context.Context, module string) ([]PermissionEntity, error) {
	query := m.DB(ctx).Where("status = ?", PermissionStatusEnabled)
	if module != "" {
		query = query.Where("module = ?", module)
	}
	var list []PermissionEntity
	err := query.Order("module ASC, id ASC").Find(&list).Error
	return list, err
}

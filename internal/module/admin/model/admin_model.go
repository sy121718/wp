package adminmodel

import (
	"context"
	"database/sql/driver"
	"errors"
	json "go_wp/pkg/jsonx"
	"time"

	adminenums "go_wp/internal/module/admin/enums"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNameSysAdmin = "sys_admin"

const (
	AdminStatusActive   = 1
	AdminStatusInactive = 2
	AdminStatusBanned   = 3
)

const allowModifyIsAdminKey string = "allow_modify_is_admin"

type JSONMap map[string]any

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

func (j *JSONMap) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("JSONMap Scan: 类型不是 []byte")
	}
	return json.Unmarshal(bytes, j)
}

type AdminEntity struct {
	ID                uint64     `gorm:"column:id;primaryKey"`
	DeptID            uint64     `gorm:"column:dept_id;default:0" datarule:"label=所属部门;ops=EQ,NEQ,IN,NOT_IN"`
	Username          string     `gorm:"column:username;uniqueIndex" datarule:"label=用户名;ops=EQ,NEQ,LIKE,NOT_LIKE"`
	Password          string     `gorm:"column:password"`
	Name              *string    `gorm:"column:name"`
	Avatar            *string    `gorm:"column:avatar"`
	Email             *string    `gorm:"column:email;index" datarule:"label=邮箱;ops=EQ,NEQ,LIKE"`
	Phone             *string    `gorm:"column:phone;index" datarule:"label=手机号;ops=EQ,NEQ"`
	Status            int        `gorm:"column:status;default:1;index" datarule:"label=状态;ops=EQ,NEQ,IN,NOT_IN"`
	IsAdmin           int        `gorm:"column:is_admin;default:0"`
	LoginFailureCount uint16     `gorm:"column:login_failure_count;default:0"`
	LockedUntilTime   *time.Time `gorm:"column:locked_until_time"`
	Metadata          JSONMap    `gorm:"column:metadata;type:json"`
	LastFailureTime   *time.Time `gorm:"column:last_failure_time"`
	RegisterIP        *string    `gorm:"column:register_ip"`
	RegisterLocation  *string    `gorm:"column:register_location"`
	LastLoginIP       *string    `gorm:"column:last_login_ip"`
	LastLoginLocation *string    `gorm:"column:last_login_location"`
	LastLoginISP      *string    `gorm:"column:last_login_isp"`
	LastLoginTime     *time.Time `gorm:"column:last_login_time"`
	CreateBy          uint64     `gorm:"column:create_by"`
	CreateTime        *time.Time `gorm:"column:create_time"`
	UpdateBy          uint64     `gorm:"column:update_by"`
	UpdateTime        *time.Time `gorm:"column:update_time"`
	Remark            *string    `gorm:"column:remark"`
}

type AdminModel struct {
	db *gorm.DB
}

func NewAdminModel(db *gorm.DB) *AdminModel {
	return &AdminModel{db: db}
}

func (m *AdminModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AdminEntity{})
}

func (m *AdminModel) DialectName() string {
	return m.db.Dialector.Name()
}

func (m *AdminModel) GetByID(ctx context.Context, id uint64) (*AdminEntity, error) {
	var entity AdminEntity
	err := m.DB(ctx).Where("id = ?", id).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

func (m *AdminModel) DeleteByIDs(ctx context.Context, ids []uint64) (deleted int64, err error) {
	result := m.DB(ctx).Where("id IN ?", ids).Delete(&AdminEntity{})
	return result.RowsAffected, result.Error
}

func (m *AdminModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *AdminModel) LockByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) ([]AdminEntity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var entities []AdminEntity
	if err := tx.WithContext(ctx).Model(&AdminEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", ids).Order("id ASC").Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, nil
}

func (m *AdminModel) DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (deleted int64, err error) {
	result := tx.WithContext(ctx).Model(&AdminEntity{}).Where("id IN ?", ids).Delete(&AdminEntity{})
	return result.RowsAffected, result.Error
}

func (m *AdminModel) IncrementLoginFailure(ctx context.Context, id uint64, lockThreshold int, lockDuration time.Duration) error {
	now := time.Now()
	lockedUntil := now.Add(lockDuration)
	return m.db.WithContext(ctx).Model(&AdminEntity{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"login_failure_count": gorm.Expr("login_failure_count + 1"),
			"last_failure_time":   now,
			"locked_until_time":   gorm.Expr("CASE WHEN login_failure_count + 1 >= ? THEN ? ELSE locked_until_time END", lockThreshold, lockedUntil),
		}).Error
}

func (m *AdminModel) ResetLoginFailure(ctx context.Context, id uint64) error {
	return m.db.WithContext(ctx).Model(&AdminEntity{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"login_failure_count": 0,
			"locked_until_time":   nil,
			"last_failure_time":   nil,
		}).Error
}

func (AdminEntity) TableName() string {
	return tableNameSysAdmin
}

func (a *AdminEntity) CanLogin() bool {
	return a.Status == AdminStatusActive && !a.IsLocked()
}

func (a *AdminEntity) IsLocked() bool {
	if a.LockedUntilTime == nil {
		return false
	}
	return time.Now().Before(*a.LockedUntilTime)
}

func (a *AdminEntity) IsActive() bool {
	return a.Status == AdminStatusActive
}

func (a *AdminEntity) IsSuperAdmin() bool {
	return a.IsAdmin == 1
}

func (a *AdminEntity) CanModifyField(field string) bool {
	protectedFields := map[string]bool{
		"IsAdmin": true,
	}
	return !protectedFields[field]
}

func (a *AdminEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	a.CreateTime = &now
	a.UpdateTime = &now

	if a.IsAdmin == 1 {
		var count int64
		if err := tx.Model(&AdminEntity{}).Where("is_admin = 1").Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return adminenums.Err(adminenums.ErrSuperAdminExists)
		}
	}
	return nil
}

func (a *AdminEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	a.UpdateTime = &now

	if tx.Statement.Changed("IsAdmin") {
		if !allowModifyIsAdmin(tx.Statement.Context) {
			return adminenums.Err(adminenums.ErrFieldProtected)
		}
	}
	return nil
}

func allowModifyIsAdmin(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value := ctx.Value(allowModifyIsAdminKey)
	allowed, ok := value.(bool)
	return ok && allowed
}

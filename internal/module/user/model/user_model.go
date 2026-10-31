package model

import (
	"context"
	"database/sql/driver"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	UserStatusDisabled = 0

	UserStatusActive = 1

	UserStatusPending = 2
)

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

type UserEntity struct {
	ID                  uint64     `gorm:"column:id;primaryKey"`
	Username            string     `gorm:"column:username"`
	Password            string     `gorm:"column:password"`
	Email               string     `gorm:"column:email"`
	EmailVerifiedAt     *time.Time `gorm:"column:email_verified_at"`
	Status              int        `gorm:"column:status;default:1"`
	Nickname            *string    `gorm:"column:nickname"`
	DisplayName         *string    `gorm:"column:display_name"`
	Avatar              *string    `gorm:"column:avatar"`
	ActivationKey       *string    `gorm:"column:activation_key"`
	ActivationExpiresAt *time.Time `gorm:"column:activation_expires_at"`
	LoginFailureCount   int        `gorm:"column:login_failure_count;default:0"`
	LockedUntilTime     *time.Time `gorm:"column:locked_until_time"`
	LastFailureTime     *time.Time `gorm:"column:last_failure_time"`
	RegisterIP          *string    `gorm:"column:register_ip"`
	RegisterLocation    *string    `gorm:"column:register_location"`
	LastLoginIP         *string    `gorm:"column:last_login_ip"`
	LastLoginLocation   *string    `gorm:"column:last_login_location"`

	RegisteredAt  *time.Time `gorm:"column:registered_at;autoCreateTime"`
	LastLoginTime *time.Time `gorm:"column:last_login_time"`
	LastActiveAt  *time.Time `gorm:"column:last_active_at"`
	Metadata      JSONMap    `gorm:"column:metadata;type:jsonb"`
	CreateBy      uint64     `gorm:"column:create_by;default:0"`
	CreateTime    *time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateTime    *time.Time `gorm:"column:update_time"`

	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

func (UserEntity) TableName() string { return "users" }

type UserModel struct{ db *gorm.DB }

func NewUserModel(db *gorm.DB) *UserModel { return &UserModel{db: db} }

func (m *UserModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&UserEntity{})
}

func (m *UserModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

const (
	EmailVerifiedAny  = 0
	EmailVerifiedOnly = 1
	EmailVerifiedNone = 2
)

type UserFilter struct {
	Keyword string

	Status int

	EmailVerified int

	RegisteredFrom *time.Time
	RegisteredTo   *time.Time

	LockedOnly bool

	Now time.Time

	IncludeDeleted bool

	UserIDs []int64
	Offset  int
	Limit   int
}

func (m *UserModel) Create(ctx context.Context, e *UserEntity) (err error) {
	return m.DB(ctx).Create(e).Error
}

func (m *UserModel) CreateTx(ctx context.Context, tx *gorm.DB, e *UserEntity) (err error) {
	return tx.WithContext(ctx).Create(e).Error
}

func (m *UserModel) GetByEmailTx(ctx context.Context, tx *gorm.DB, email string) (e *UserEntity, err error) {
	addr := strings.TrimSpace(email)
	if addr == "" {
		return nil, gorm.ErrRecordNotFound
	}
	e = &UserEntity{}
	err = tx.WithContext(ctx).Where("lower(email) = lower(?)", addr).First(e).Error
	return e, err
}

func (m *UserModel) GetByUsernameTx(ctx context.Context, tx *gorm.DB, username string) (e *UserEntity, err error) {
	e = &UserEntity{}
	err = tx.WithContext(ctx).Where("lower(username) = lower(?)", username).First(e).Error
	return e, err
}

func (m *UserModel) GetByID(ctx context.Context, id uint64) (e *UserEntity, err error) {
	e = &UserEntity{}
	err = m.DB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *UserModel) GetByUsername(ctx context.Context, username string) (e *UserEntity, err error) {
	e = &UserEntity{}
	err = m.DB(ctx).Where("lower(username) = lower(?)", strings.TrimSpace(username)).First(e).Error
	return e, err
}

func (m *UserModel) GetByEmail(ctx context.Context, email string) (e *UserEntity, err error) {
	addr := strings.TrimSpace(email)
	if addr == "" {
		return nil, gorm.ErrRecordNotFound
	}
	e = &UserEntity{}
	err = m.DB(ctx).Where("lower(email) = lower(?)", addr).First(e).Error
	return e, err
}

func (m *UserModel) GetByActivationKey(ctx context.Context, key string) (e *UserEntity, err error) {
	e = &UserEntity{}
	err = m.DB(ctx).Where("activation_key = ?", strings.TrimSpace(key)).First(e).Error
	return e, err
}

func (m *UserModel) List(ctx context.Context, f UserFilter) (list []*UserEntity, total int64, err error) {
	q := m.DB(ctx)
	if f.IncludeDeleted {
		q = q.Unscoped()
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {

		q = q.Where("search_text LIKE ?", "%"+strings.ToLower(kw)+"%")
	}
	if f.Status >= 0 {
		q = q.Where("status = ?", f.Status)
	}
	switch f.EmailVerified {
	case EmailVerifiedOnly:
		q = q.Where("email_verified_at IS NOT NULL")
	case EmailVerifiedNone:
		q = q.Where("email_verified_at IS NULL")
	}
	if f.LockedOnly {
		now := f.Now
		if now.IsZero() {
			now = time.Now()
		}
		q = q.Where("locked_until_time IS NOT NULL AND locked_until_time > ?", now)
	}
	if f.RegisteredFrom != nil {
		q = q.Where("registered_at >= ?", *f.RegisteredFrom)
	}
	if f.RegisteredTo != nil {
		q = q.Where("registered_at <= ?", *f.RegisteredTo)
	}
	if f.UserIDs != nil {
		if len(f.UserIDs) == 0 {

			return []*UserEntity{}, 0, nil
		}
		q = q.Where("id IN ?", f.UserIDs)
	}
	if err = q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	err = q.Order("id DESC").Offset(f.Offset).Limit(limit).Find(&list).Error
	return list, total, err
}

func (m *UserModel) UpdateFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.DB(ctx).Where("id = ?", id).Updates(fields).Error
}

func (m *UserModel) UpdateFieldsTx(ctx context.Context, tx *gorm.DB, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Model(&UserEntity{}).Where("id = ?", id).Updates(fields).Error
}

func (m *UserModel) SoftDelete(ctx context.Context, id uint64, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).Update("deleted_at", at).Error
}

func (m *UserModel) Restore(ctx context.Context, id uint64) (err error) {
	return m.db.WithContext(ctx).Unscoped().Model(&UserEntity{}).
		Where("id = ?", id).Update("deleted_at", nil).Error
}

func (m *UserModel) CountByExistence(ctx context.Context, username, email string, excludeID uint64) (count int64, err error) {

	group := m.db.WithContext(ctx).Where("lower(username) = lower(?)", strings.TrimSpace(username))
	if addr := strings.TrimSpace(email); addr != "" {
		group = group.Or("lower(email) = lower(?)", addr)
	}
	q := m.db.WithContext(ctx).Unscoped().Model(&UserEntity{}).Where(group)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err = q.Count(&count).Error
	return count, err
}

func (m *UserModel) IncrLoginFailure(ctx context.Context, id uint64, lockThreshold int, lockDuration time.Duration) (err error) {
	now := time.Now()
	lockedUntil := now.Add(lockDuration)
	return m.DB(ctx).Where("id = ?", id).
		Updates(map[string]any{
			"login_failure_count": gorm.Expr("login_failure_count + 1"),
			"last_failure_time":   now,
			"locked_until_time": gorm.Expr(
				"CASE WHEN login_failure_count + 1 >= ? THEN ? ELSE locked_until_time END",
				lockThreshold, lockedUntil),
		}).Error
}

func (m *UserModel) ResetLoginFailure(ctx context.Context, id uint64) (err error) {
	return m.DB(ctx).Where("id = ?", id).
		Updates(map[string]any{
			"login_failure_count": 0,
			"locked_until_time":   nil,
			"last_failure_time":   nil,
		}).Error
}

func (m *UserModel) RecordLogin(ctx context.Context, id uint64, ip, location string, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).
		Updates(map[string]any{
			"last_login_ip":       ip,
			"last_login_location": location,
			"last_login_time":     at,
			"last_active_at":      at,
		}).Error
}

func (m *UserModel) TouchActive(ctx context.Context, id uint64, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).Update("last_active_at", at).Error
}

type CustomerCounters struct {
	Total      int64 `gorm:"column:total"`
	Active     int64 `gorm:"column:active"`
	Disabled   int64 `gorm:"column:disabled"`
	Pending    int64 `gorm:"column:pending"`
	Locked     int64 `gorm:"column:locked"`
	Verified   int64 `gorm:"column:verified"`
	Unverified int64 `gorm:"column:unverified"`
}

func (m *UserModel) CountCustomers(ctx context.Context, now time.Time) (c CustomerCounters, err error) {
	if err = m.DB(ctx).
		Select("COUNT(*) AS total, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS active, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS disabled, "+
			"COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS pending, "+
			"COALESCE(SUM(CASE WHEN email_verified_at IS NOT NULL THEN 1 ELSE 0 END), 0) AS verified, "+
			"COALESCE(SUM(CASE WHEN locked_until_time IS NOT NULL AND locked_until_time > ? THEN 1 ELSE 0 END), 0) AS locked",
			UserStatusActive, UserStatusDisabled, UserStatusPending, now).
		Scan(&c).Error; err != nil {
		return c, err
	}

	c.Unverified = c.Total - c.Verified
	return c, nil
}

func (m *UserModel) SetStatus(ctx context.Context, id uint64, status int) (err error) {
	return m.DB(ctx).Where("id = ?", id).Update("status", status).Error
}

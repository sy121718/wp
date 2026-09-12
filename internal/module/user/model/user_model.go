package model

// user_model.go — 访客账号的表访问单元（issue #36）。
//
// 定位照 AGENTS.md「model 层定位」：这里是 Repository，不是领域模型 ——
// 只做本模块表的 CRUD 与通用查询，条件一律以参数传入，业务规则（谁能改、状态机）留在 service。

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// 用户状态。
const (
	// UserStatusDisabled 禁用：不能登录，历史数据保留（与「删除」区分）。
	UserStatusDisabled = 0
	// UserStatusActive 正常。
	UserStatusActive = 1
	// UserStatusPending 待激活：注册后未完成邮箱验证，不能登录。
	UserStatusPending = 2
)

const ()

// JSONMap 可序列化的 JSON 扩展字段（实现 sql.Scanner / driver.Valuer）。
//
// 与 admin 模块同名类型是**有意重复**：跨模块不得 import 对方的 model
// （AGENTS.md「表隔离约定」），这点重复换来的是模块可独立演进。
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

// UserEntity 对应 users 表（身份与认证）。
type UserEntity struct {
	ID                  uint64     `gorm:"column:id;primaryKey"`
	Username            string     `gorm:"column:username;type:varchar(60)"`
	Password            string     `gorm:"column:password;type:varchar(100)"`
	Email               string     `gorm:"column:email;type:varchar(100)"`
	EmailVerifiedAt     *time.Time `gorm:"column:email_verified_at;type:timestamp(3)"`
	Status              int        `gorm:"column:status;type:smallint;default:1"`
	Nickname            *string    `gorm:"column:nickname;type:varchar(60)"`
	DisplayName         *string    `gorm:"column:display_name;type:varchar(250)"`
	Avatar              *string    `gorm:"column:avatar;type:varchar(255)"`
	ActivationKey       *string    `gorm:"column:activation_key;type:varchar(64)"`
	ActivationExpiresAt *time.Time `gorm:"column:activation_expires_at;type:timestamp(3)"`
	LoginFailureCount   int        `gorm:"column:login_failure_count;type:integer;default:0"`
	LockedUntilTime     *time.Time `gorm:"column:locked_until_time;type:timestamp(3)"`
	LastFailureTime     *time.Time `gorm:"column:last_failure_time;type:timestamp(3)"`
	RegisterIP          *string    `gorm:"column:register_ip;type:varchar(50)"`
	RegisterLocation    *string    `gorm:"column:register_location;type:varchar(100)"`
	LastLoginIP         *string    `gorm:"column:last_login_ip;type:varchar(50)"`
	LastLoginLocation   *string    `gorm:"column:last_login_location;type:varchar(100)"`
	RegisteredAt        *time.Time `gorm:"column:registered_at;type:timestamp(3)"`
	LastLoginTime       *time.Time `gorm:"column:last_login_time;type:timestamp(3)"`
	LastActiveAt        *time.Time `gorm:"column:last_active_at;type:timestamp(3)"`
	Metadata            JSONMap    `gorm:"column:metadata;type:jsonb"`
	CreateBy            uint64     `gorm:"column:create_by;type:bigint;default:0"`
	CreateTime          *time.Time `gorm:"column:create_time;type:timestamp(3)"`
	UpdateTime          *time.Time `gorm:"column:update_time;type:timestamp(3)"`
}

// TableName 表名（迁移 123）。
func (UserEntity) TableName() string { return "users" }

// UserModel users 表访问单元。
type UserModel struct{ db *gorm.DB }

// NewUserModel 构造。
func NewUserModel(db *gorm.DB) *UserModel { return &UserModel{db: db} }

// DB 返回绑定本表的句柄（**只允许本 model 的仓储方法消费**，service 不得调用）。
func (m *UserModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&UserEntity{})
}

// Transaction 透传事务：跨表编排由 service 决定边界（model 不自己开事务）。
func (m *UserModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// UserFilter 列表筛选（条件以参数传入，方法内不写死业务条件）。
type UserFilter struct {
	// Keyword 模糊匹配 登录名 / 邮箱 / 昵称（大小写不敏感）。
	Keyword string
	// Status <0 表示不过滤。
	Status int
	Offset int
	Limit  int
}

// Create 新建用户（唯一索引冲突由 service 转成业务错误）。
func (m *UserModel) Create(ctx context.Context, e *UserEntity) (err error) {
	return m.DB(ctx).Create(e).Error
}

// GetByID 按主键取（不存在返回 gorm.ErrRecordNotFound）。
func (m *UserModel) GetByID(ctx context.Context, id uint64) (e *UserEntity, err error) {
	e = &UserEntity{}
	err = m.DB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// GetByUsername 按登录名取（大小写不敏感 —— 与迁移 123 的 lower() 唯一索引同口径）。
func (m *UserModel) GetByUsername(ctx context.Context, username string) (e *UserEntity, err error) {
	e = &UserEntity{}
	err = m.DB(ctx).Where("lower(username) = lower(?)", strings.TrimSpace(username)).First(e).Error
	return e, err
}

// GetByEmail 按邮箱取（同样大小写不敏感）。
//
// **空邮箱直接返回未找到**：第三方注册的账号可能没有邮箱（微信 / QQ 默认不返回），
// 而迁移 123 里空串是允许重复的 —— 若拿空串去查，会把**另一个也没邮箱的账号**匹配出来，
// 等于串号。空邮箱不是有效的查询条件。
func (m *UserModel) GetByEmail(ctx context.Context, email string) (e *UserEntity, err error) {
	addr := strings.TrimSpace(email)
	if addr == "" {
		return nil, gorm.ErrRecordNotFound
	}
	e = &UserEntity{}
	err = m.DB(ctx).Where("lower(email) = lower(?)", addr).First(e).Error
	return e, err
}

// GetByActivationKey 按激活凭据取（用于激活 / 重置密码）。
func (m *UserModel) GetByActivationKey(ctx context.Context, key string) (e *UserEntity, err error) {
	e = &UserEntity{}
	err = m.DB(ctx).Where("activation_key = ?", strings.TrimSpace(key)).First(e).Error
	return e, err
}

// List 按筛选条件分页取用户（返回列表与总数）。
func (m *UserModel) List(ctx context.Context, f UserFilter) (list []*UserEntity, total int64, err error) {
	q := m.DB(ctx)
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		// ILIKE 的等价写法：位置参数化，不做字符串拼接。
		like := "%" + kw + "%"
		q = q.Where("username ILIKE ? OR email ILIKE ? OR nickname ILIKE ?", like, like, like)
	}
	if f.Status >= 0 {
		q = q.Where("status = ?", f.Status)
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

// UpdateFields 按主键更新指定列（列与值都以参数传入，调用方决定改什么）。
func (m *UserModel) UpdateFields(ctx context.Context, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return m.DB(ctx).Where("id = ?", id).Updates(fields).Error
}

// UpdateFieldsTx 事务内按主键更新（跨表编排用，如「改资料 + 记审计」）。
//
// ctx 照常传入：事务里同样要能取消、要带审计信息，不能悄悄换成 Background。
func (m *UserModel) UpdateFieldsTx(ctx context.Context, tx *gorm.DB, id uint64, fields map[string]any) (err error) {
	if len(fields) == 0 {
		return nil
	}
	return tx.WithContext(ctx).Model(&UserEntity{}).Where("id = ?", id).Updates(fields).Error
}

// Delete 删除用户（级联清理由 service 在事务内编排）。
func (m *UserModel) Delete(ctx context.Context, id uint64) (err error) {
	return m.DB(ctx).Where("id = ?", id).Delete(&UserEntity{}).Error
}

// CountByExistence 统计登录名 / 邮箱冲突的其它用户数（唯一性校验用，排除自身）。
//
// 邮箱为空时不参与判断：空邮箱在库里允许重复（第三方账号可能没有邮箱），
// 拿它去比对会把两个都没邮箱的账号判成冲突。
func (m *UserModel) CountByExistence(ctx context.Context, username, email string, excludeID uint64) (count int64, err error) {
	q := m.DB(ctx).Where("lower(username) = lower(?)", strings.TrimSpace(username))
	if addr := strings.TrimSpace(email); addr != "" {
		q = q.Or("lower(email) = lower(?)", addr)
	}
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err = q.Count(&count).Error
	return count, err
}

// IncrLoginFailure 原子递增登录失败计数（禁止读-改-写回，见 AGENTS.md「登录安全」）。
func (m *UserModel) IncrLoginFailure(ctx context.Context, id uint64, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).
		Updates(map[string]any{
			"login_failure_count": gorm.Expr("login_failure_count + 1"),
			"last_failure_time":   at,
		}).Error
}

// ResetLoginFailure 登录成功后清零失败计数与锁定。
func (m *UserModel) ResetLoginFailure(ctx context.Context, id uint64) (err error) {
	return m.DB(ctx).Where("id = ?", id).
		Updates(map[string]any{"login_failure_count": 0, "locked_until_time": nil}).Error
}

// SetLockedUntil 设置锁定截止时间（只写这一个列，**绝不改 status** —— 见 AGENTS.md「登录安全」）。
func (m *UserModel) SetLockedUntil(ctx context.Context, id uint64, until time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).Update("locked_until_time", until).Error
}

// RecordLogin 记录一次成功登录（IP / 归属地 / 时间 / 活跃时间一次写完）。
func (m *UserModel) RecordLogin(ctx context.Context, id uint64, ip, location string, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).
		Updates(map[string]any{
			"last_login_ip":       ip,
			"last_login_location": location,
			"last_login_time":     at,
			"last_active_at":      at,
		}).Error
}

// TouchActive 更新最后活跃时间（轻量，供心跳类调用）。
func (m *UserModel) TouchActive(ctx context.Context, id uint64, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).Update("last_active_at", at).Error
}

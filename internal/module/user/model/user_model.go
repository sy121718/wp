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
	// RegisteredAt 是 NOT NULL 列：必须由 model 自己兜底填值，
	// 否则调用方漏填时 GORM 会显式插入 NULL 撞约束（不是「用数据库默认值」，
	// 显式列在 INSERT 列表里就会覆盖掉 DEFAULT CURRENT_TIMESTAMP）。
	RegisteredAt  *time.Time `gorm:"column:registered_at;type:timestamp(3);autoCreateTime"`
	LastLoginTime *time.Time `gorm:"column:last_login_time;type:timestamp(3)"`
	LastActiveAt  *time.Time `gorm:"column:last_active_at;type:timestamp(3)"`
	Metadata      JSONMap    `gorm:"column:metadata;type:jsonb"`
	CreateBy      uint64     `gorm:"column:create_by;type:bigint;default:0"`
	CreateTime    *time.Time `gorm:"column:create_time;type:timestamp(3);autoCreateTime"`
	UpdateTime    *time.Time `gorm:"column:update_time;type:timestamp(3)"`
	// DeletedAt 注销时间（**软删除**：数据保留，只是不再可见）。
	//
	// 用 GORM 的软删除类型：Delete 自动变成 UPDATE deleted_at，所有查询自动加
	// `deleted_at IS NULL`（注销的账号自然登录不上、列表里也不出现），
	// 需要看已注销的用 Unscoped()。
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;type:timestamp(3);index"`
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

// 邮箱验证筛选的取值。
//
// 「不过滤」必须是 0（零值）：把它写成 1/0 表示「已验证/未验证」的话，
// 任何忘了设这个字段的调用方都会静默变成「只看未验证」——
// 而后台客户列表正是「不设这个字段就显示全部」的那种用法。
const (
	EmailVerifiedAny  = 0
	EmailVerifiedOnly = 1
	EmailVerifiedNone = 2
)

// UserFilter 列表筛选（条件以参数传入，方法内不写死业务条件）。
type UserFilter struct {
	// Keyword 模糊匹配 登录名 / 邮箱 / 昵称 / 展示名（大小写不敏感）。
	//
	// 展示名也要匹配：后台是按「客户叫什么」来找人的，而展示名往往才是他在页面上
	// 留下的那个名字 —— 只搜昵称会让「明明有这个客户却搜不到」。
	Keyword string
	// Status <0 表示不过滤。
	Status int
	// EmailVerified EmailVerifiedAny / Only / None（0 = 不过滤）。
	// 判定看 email_verified_at 是否为空，与 service 侧的「已验证」口径同源。
	EmailVerified int
	// RegisteredFrom / RegisteredTo 注册时间范围（闭区间，nil = 该端不限）。
	RegisteredFrom *time.Time
	RegisteredTo   *time.Time
	// IncludeDeleted 是否包含**已注销**用户（默认不含）。
	//
	// 管理员有时要查「谁注销过」，所以这里给一个显式开关，而不是让默认查询看得见 ——
	// 默认可见会让各处忘记过滤，把注销用户当成正常用户。
	IncludeDeleted bool
	Offset         int
	Limit          int
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
	if f.IncludeDeleted {
		q = q.Unscoped()
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		// 四列各建一个 GIN 索引意味着每次写入要维护四个索引，查询仍要 OR 四路；
		// 因此迁移 167 加了一个生成列 search_text（username/email/nickname/display_name
		// 拼接并小写），这里对它做一次匹配：语义仍是「四列中任一包含关键词」，
		// 但只走一个索引。大小写不敏感由生成列的 lower() 承担，参数同样小写。
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
	if f.RegisteredFrom != nil {
		q = q.Where("registered_at >= ?", *f.RegisteredFrom)
	}
	if f.RegisteredTo != nil {
		q = q.Where("registered_at <= ?", *f.RegisteredTo)
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

// SoftDelete 注销用户（**软删除：数据不删**，只写 deleted_at）。
//
// 关联网的处理（会话全部失效、应用密码吊销）由 service 在同一事务内编排 ——
// model 只负责本表这一行。
func (m *UserModel) SoftDelete(ctx context.Context, id uint64, at time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).Update("deleted_at", at).Error
}

// Restore 撤销注销（管理员用：误注销 / 客服申诉恢复）。
//
// 必须 Unscoped：默认查询条件会自动带上 `deleted_at IS NULL`，
// 那正好会把要恢复的那一行排除在外。
func (m *UserModel) Restore(ctx context.Context, id uint64) (err error) {
	return m.db.WithContext(ctx).Unscoped().Model(&UserEntity{}).
		Where("id = ?", id).Update("deleted_at", nil).Error
}

// CountByExistence 统计登录名 / 邮箱冲突的其它用户数（唯一性校验用，排除自身）。
//
// **必须 Unscoped（含已注销用户）**：用户名与邮箱对注销用户是**永久占用**的
// （见迁移 123 的注释：防「顶着刚注销的名字」冒充），而数据库唯一索引建在表上、
// 不区分是否注销 —— 若这里跟着默认的软删除过滤走，应用层会判断「可以用」、
// 数据库唯一索引却拒绝插入，最后抛给用户一个难懂的 DB 错误。
// 两边的口径必须一致：**注册查重看全表，登录才只看未注销的**。
//
// 邮箱为空时不参与判断：空邮箱在库里允许重复（第三方账号可能没有邮箱），
// 拿它去比对会把两个都没邮箱的账号判成冲突。
func (m *UserModel) CountByExistence(ctx context.Context, username, email string, excludeID uint64) (count int64, err error) {
	// 「用户名命中 或 邮箱命中」必须整体成组，再加上「排除自身」。
	//
	// 不能写成链式 Where(用户名).Or(邮箱).Where(id <> ?)：GORM 会拼出
	// `lower(username) = ? OR lower(email) = ? AND id <> ?`，而 SQL 里 AND 优先级高于 OR，
	// 实际语义变成 `用户名命中 OR (邮箱命中 AND 不是自己)` —— 自己那行会被用户名条件捞回来，
	// 于是「只改昵称、用户名邮箱原样提交」这种最常见的改资料操作会被判成自己与自己冲突。
	// 分组靠把子条件当参数传给 Where 实现（GORM 会为其加括号）。
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

// IncrLoginFailure 原子累加登录失败计数，连续达到阈值时锁定（口径与 admin 逐字一致）。
//
// 必须用**单条**原子 SQL（count = count + 1）而非读-改-写：并发失败请求读到相同计数会丢计数，
// 结果是永远触发不了锁定，可被无限暴力破解。锁定条件也写在同一条 SQL 里（CASE WHEN）——
// 「先加计数、再读回来判断要不要锁」是同一类竞态，只是换了个位置。
//
// 锁定**只写 locked_until_time，绝不修改 status**：status 表达的是管理状态
// （正常 / 禁用），把它改成「锁定」之后到期也不会自己变回来，一次失败就能永久锁死账号。
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

// ResetLoginFailure 登录成功后原子清零失败计数与锁定（含 last_failure_time，不留半截状态）。
func (m *UserModel) ResetLoginFailure(ctx context.Context, id uint64) (err error) {
	return m.DB(ctx).Where("id = ?", id).
		Updates(map[string]any{
			"login_failure_count": 0,
			"locked_until_time":   nil,
			"last_failure_time":   nil,
		}).Error
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

// CustomerCounters 客户账号的分布计数（后台客户列表页的计数条）。
type CustomerCounters struct {
	Total      int64 `gorm:"column:total"`
	Active     int64 `gorm:"column:active"`
	Disabled   int64 `gorm:"column:disabled"`
	Pending    int64 `gorm:"column:pending"`
	Locked     int64 `gorm:"column:locked"`
	Verified   int64 `gorm:"column:verified"`
	Unverified int64 `gorm:"column:unverified"`
}

// CountCustomers 统计未注销账号的状态 / 邮箱验证 / 锁定分布。
//
// now 由调用方传入，不在方法里取时间：判「是否锁定」比较的是 locked_until_time > now，
// 若同一个请求里两处各自取时间，跨过锁定到期那一刻时页面会自相矛盾
// （计数条说 1 个已锁定，列表里那一行却显示「未锁定」）。
//
// 一条 SQL 出全部计数：分几次查只会在两次之间被并发注册/失败登录插进来，
// 于是「总数」与「各状态之和」对不上。
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
	// 未验证 = 总数 - 已验证：在 Go 里减而不是再加一条 SQL 条件，
	// 两者的口径因此不可能分叉（少一项 COUNT 就少一处能写错的地方）。
	c.Unverified = c.Total - c.Verified
	return c, nil
}

// SetStatus 改账号状态（管理侧：正常 / 已停用）。
//
// 只动 status 一列，**不碰锁定字段**：那是另一条轴 ——
// 把它们混在一起，就会出现「解除停用之后账号还带着上一次的失败计数」这种状态，
// 于是「刚恢复的账号第一次输错密码就被锁」。
func (m *UserModel) SetStatus(ctx context.Context, id uint64, status int) (err error) {
	return m.DB(ctx).Where("id = ?", id).Update("status", status).Error
}

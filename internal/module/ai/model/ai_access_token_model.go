// ai_access_token_model.go — 对外访问令牌（迁移 542）。
//
// 这张表是**外部调用**的身份载体：站内会话的身份是登录态，外部 harness 没有登录态，
// 拿的就是这里的令牌。因此它与 ai_session 单表一样，是全局对象、不带 project_id、不受 RLS。
//
// 三条安全约定（改动前先读）：
//
//  1. **明文永不落库**：库里只有 token_hash（SHA-256）。校验用哈希比对，
//     列表与详情一律不回哈希（它虽不可逆，但泄漏后可用于离线爆破弱令牌）。
//  2. **前缀只用于展示**：token_prefix 用来在列表里区分同名令牌与排错，
//     不允许把它当校验依据（校验一律走 hash 的唯一索引）。
//  3. **撤销不删行**：status=0 + revoked_time，审计才能回答「这把我什么时候撤的」。
//
// 只增不改的部分：本文件不提供物理删除（Delete 不存在即是最强约束）。
package aimodel

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
)

const tableNameAIAccessToken = "ai_access_token"

// 落库取值（与 aienums.TokenStatus 同值）：model 层不 import enums（同包 session 也是这么分的），
// 因此这里再写一遍字面量 —— 它们是**落库值**的唯一来源，改一处要同步改 enums 与迁移 542 的注释。
const (
	tokenStatusRevoked int16 = 0
	tokenStatusActive  int16 = 1
)

// StringList 把 jsonb 数组与 []string 对上（写法与同包的 JSONMap 一致）。
//
// 为什么不直接用 json.RawMessage：scopes 在业务侧要被逐个比对（「这个令牌有没有 order:list」），
// 每次调用都手工 unmarshal 会把解析错误散到多个调用点；类型自带的 Scan/Value 让它只在这里发生一次。
type StringList []string

// Value 写库：[]string → jsonb。
func (l StringList) Value() (driver.Value, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]string(l))
}

// Scan 读库：jsonb → []string。空值与 NULL 都读成空切片（不是 nil，免得多一个判空分支）。
func (l *StringList) Scan(value any) error {
	if value == nil {
		*l = StringList{}
		return nil
	}
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return errors.New("ai_access_token.scopes 列类型不认识")
	}
	if len(raw) == 0 {
		*l = StringList{}
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*l = StringList(out)
	return nil
}

// AIAccessTokenEntity 对应 ai_access_token 表（列型真相在迁移 542；model 不声明列型）。
type AIAccessTokenEntity struct {
	ID int64 `gorm:"column:id;primaryKey"`
	// Name 用途备注；UserID 归属账号（权限判定与审计都顺着它找人）。
	Name   string `gorm:"column:name"`
	UserID int64  `gorm:"column:user_id"`
	// TokenPrefix 明文前若干位（仅供展示）；TokenHash 是明文的 SHA-256。
	TokenPrefix string `gorm:"column:token_prefix"`
	TokenHash   string `gorm:"column:token_hash"`
	// Scopes 权限点子集；实际可用 = 本集合 ∩ 归属账号仍拥有的权限。
	Scopes StringList `gorm:"column:scopes;type:jsonb"`
	// Status 取值见 aienums.TokenStatus（本层只存 int16，不依赖 enums —— 与 session 同口径）。
	Status int16 `gorm:"column:status"`
	// ExpiresAt 过期时刻；nil = 不过期。LastUsedTime 最近一次成功使用；RevokedTime 撤销时刻。
	ExpiresAt    *time.Time `gorm:"column:expires_at"`
	LastUsedTime *time.Time `gorm:"column:last_used_time"`
	RevokedTime  *time.Time `gorm:"column:revoked_time"`
	CreateTime   time.Time  `gorm:"column:create_time;autoCreateTime"`
	UpdateTime   time.Time  `gorm:"column:update_time;autoUpdateTime"`
}

// TableName 表名。
func (AIAccessTokenEntity) TableName() string { return tableNameAIAccessToken }

// AccessTokenModel 负责 ai_access_token 一张表。
type AccessTokenModel struct {
	db *gorm.DB
}

// NewAccessTokenModel 构造。
func NewAccessTokenModel(db *gorm.DB) *AccessTokenModel { return &AccessTokenModel{db: db} }

func (m *AccessTokenModel) tokens(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AIAccessTokenEntity{})
}

// Insert 新增一把令牌。哈希撞车（唯一索引冲突）会作为错误冒上来 ——
// 那意味着随机数生成出了问题，静默重试不如让它响。
func (m *AccessTokenModel) Insert(ctx context.Context, e *AIAccessTokenEntity) error {
	if e == nil {
		return nil
	}
	return m.tokens(ctx).Create(e).Error
}

// FindByHash 按哈希查一把令牌（校验路径）。
//
// 走 uq_ai_access_token_hash 唯一索引：**不用前缀查候选再比对** ——
// 前缀不是唯一键，同前缀多行时要在 Go 侧逐个比对（还带时序侧信道），
// 而哈希本身就能一击命中。找不到回 (nil, nil)，由调用方决定文案。
func (m *AccessTokenModel) FindByHash(ctx context.Context, hash string) (*AIAccessTokenEntity, error) {
	if hash == "" {
		return nil, nil
	}
	var row AIAccessTokenEntity
	err := m.tokens(ctx).Where("token_hash = ?", hash).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// List 取令牌列表（倒序，最近创建的在最前）。
//
// userID > 0 时只取该账号的；userID <= 0 取全部（管理页看全站）。
// limit <= 0 回落 50（管理页一屏的量级）。
func (m *AccessTokenModel) List(ctx context.Context, userID int64, limit int) (rows []AIAccessTokenEntity, err error) {
	if limit <= 0 {
		limit = 50
	}
	q := m.tokens(ctx)
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	err = q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// Revoke 撤销一把令牌（status=0 + revoked_time）。
//
// 已撤销的行不重复写 revoked_time（用 WHERE status = 1 限定）：
// 「什么时候撤的」只该有一个答案，重复点击不该把它改成新的时刻。
// 返回受影响行数，调用方据此区分「撤销成功」与「它本来就不存在/已撤销」。
func (m *AccessTokenModel) Revoke(ctx context.Context, id int64) (int64, error) {
	if id <= 0 {
		return 0, nil
	}
	res := m.tokens(ctx).Where("id = ? AND status = ?", id, tokenStatusActive).
		Updates(map[string]any{"status": tokenStatusRevoked, "revoked_time": time.Now()})
	return res.RowsAffected, res.Error
}

// TouchUsed 记一次成功使用（last_used_time）。
//
// 失败不冒给调用方：这是旁路观测（「发了没用」靠它识别），
// 写不进去不该让一次正常的工具调用失败。
func (m *AccessTokenModel) TouchUsed(ctx context.Context, id int64) error {
	if id <= 0 {
		return nil
	}
	return m.tokens(ctx).Where("id = ?", id).
		Update("last_used_time", time.Now()).Error
}

package aimodel

import (
	"context"
	"database/sql/driver"
	"errors"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"
)

const tableNameAIAccessToken = "ai_access_token"

const (
	tokenStatusRevoked int16 = 0
	tokenStatusActive  int16 = 1
)

type StringList []string

func (l StringList) Value() (driver.Value, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]string(l))
}

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

type AIAccessTokenEntity struct {
	ID int64 `gorm:"column:id;primaryKey"`

	Name   string `gorm:"column:name"`
	UserID int64  `gorm:"column:user_id"`

	TokenPrefix string `gorm:"column:token_prefix"`
	TokenHash   string `gorm:"column:token_hash"`

	Scopes StringList `gorm:"column:scopes;type:jsonb"`

	Status int16 `gorm:"column:status"`

	ExpiresAt    *time.Time `gorm:"column:expires_at"`
	LastUsedTime *time.Time `gorm:"column:last_used_time"`
	RevokedTime  *time.Time `gorm:"column:revoked_time"`
	CreateTime   time.Time  `gorm:"column:create_time;autoCreateTime"`
	UpdateTime   time.Time  `gorm:"column:update_time;autoUpdateTime"`
}

func (AIAccessTokenEntity) TableName() string { return tableNameAIAccessToken }

type AccessTokenModel struct {
	db *gorm.DB
}

func NewAccessTokenModel(db *gorm.DB) *AccessTokenModel { return &AccessTokenModel{db: db} }

func (m *AccessTokenModel) tokens(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AIAccessTokenEntity{})
}

func (m *AccessTokenModel) Insert(ctx context.Context, e *AIAccessTokenEntity) error {
	if e == nil {
		return nil
	}
	return m.tokens(ctx).Create(e).Error
}

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

func (m *AccessTokenModel) Revoke(ctx context.Context, id int64) (int64, error) {
	if id <= 0 {
		return 0, nil
	}
	res := m.tokens(ctx).Where("id = ? AND status = ?", id, tokenStatusActive).
		Updates(map[string]any{"status": tokenStatusRevoked, "revoked_time": time.Now()})
	return res.RowsAffected, res.Error
}

func (m *AccessTokenModel) TouchUsed(ctx context.Context, id int64) error {
	if id <= 0 {
		return nil
	}
	return m.tokens(ctx).Where("id = ?", id).
		Update("last_used_time", time.Now()).Error
}

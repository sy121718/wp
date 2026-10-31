package aimodel

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"
)

type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(value any) error {
	if value == nil {
		*m = nil
		return nil
	}
	raw, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("JSONMap Scan: 类型不是 []byte")
	}
	if len(raw) == 0 {
		*m = JSONMap{}
		return nil
	}
	var out JSONMap
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*m = out
	return nil
}

type AIProviderEntity struct {
	ID           int64     `gorm:"column:id;primaryKey"`
	ProviderKey  string    `gorm:"column:provider_key"`
	DisplayName  string    `gorm:"column:display_name"`
	BaseURL      string    `gorm:"column:base_url"`
	Protocol     string    `gorm:"column:protocol"`
	APIKeyCipher string    `gorm:"column:api_key_cipher"`
	Status       int       `gorm:"column:status"`
	Sort         int       `gorm:"column:sort"`
	ConfigData   JSONMap   `gorm:"column:config_data;type:jsonb"`
	Version      int64     `gorm:"column:version"`
	CreateBy     int64     `gorm:"column:create_by"`
	CreateTime   time.Time `gorm:"column:create_time;autoCreateTime"`
	UpdateBy     int64     `gorm:"column:update_by"`
	UpdateTime   time.Time `gorm:"column:update_time;autoUpdateTime"`
}

func (AIProviderEntity) TableName() string { return "ai_provider" }

type Model struct {
	db *gorm.DB
}

func NewAIModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AIProviderEntity{})
}

func (m *Model) FindByID(ctx context.Context, id int64) (*AIProviderEntity, error) {
	var e AIProviderEntity
	err := m.DB(ctx).Where("id = ?", id).Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (m *Model) FindByKey(ctx context.Context, providerKey string) (*AIProviderEntity, error) {
	var e AIProviderEntity
	err := m.DB(ctx).Where("provider_key = ?", providerKey).Take(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (m *Model) ListAll(ctx context.Context) ([]AIProviderEntity, error) {
	rows := make([]AIProviderEntity, 0, 8)
	if err := m.DB(ctx).Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (m *Model) Create(ctx context.Context, e *AIProviderEntity) error {
	return m.DB(ctx).Create(e).Error
}

func (m *Model) UpdateFields(ctx context.Context, id, version int64, fields map[string]any, updateBy int64) (int64, error) {
	updates := make(map[string]any, len(fields)+3)
	for k, v := range fields {
		updates[k] = v
	}
	updates["version"] = gorm.Expr("version + 1")
	updates["update_by"] = updateBy
	updates["update_time"] = gorm.Expr("now()")

	res := m.DB(ctx).Where("id = ? AND version = ?", id, version).Updates(updates)
	return res.RowsAffected, res.Error
}

func (m *Model) DeleteByID(ctx context.Context, id int64) (int64, error) {
	res := m.DB(ctx).Where("id = ?", id).Delete(&AIProviderEntity{})
	return res.RowsAffected, res.Error
}

func (m *Model) VersionOf(ctx context.Context, id int64) (version int64, found bool, err error) {
	var e AIProviderEntity
	terr := m.DB(ctx).Select("id", "version").Where("id = ?", id).Take(&e).Error
	if errors.Is(terr, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if terr != nil {
		return 0, false, terr
	}
	return e.Version, true, nil
}

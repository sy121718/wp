package sysconfigmodel

import (
	"context"
	"database/sql/driver"
	"errors"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"
)

const tableNameSysConfig = "sys_config"

const (
	StatusDisabled = 0

	StatusEnabled = 1
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

type SysConfigEntity struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	GroupKey   string    `gorm:"column:group_key"`
	GroupName  string    `gorm:"column:group_name"`
	ConfigData JSONMap   `gorm:"column:config_data;type:jsonb"`
	Remark     string    `gorm:"column:remark"`
	Status     int       `gorm:"column:status"`
	Version    int64     `gorm:"column:version"`
	CreateBy   int64     `gorm:"column:create_by"`
	CreateTime time.Time `gorm:"column:create_time"`
	UpdateBy   int64     `gorm:"column:update_by"`
	UpdateTime time.Time `gorm:"column:update_time"`
}

func (SysConfigEntity) TableName() string { return tableNameSysConfig }

type Model struct {
	db *gorm.DB
}

func NewSysConfigModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&SysConfigEntity{})
}

func (m *Model) FindByKey(ctx context.Context, groupKey string) (*SysConfigEntity, error) {
	var e SysConfigEntity
	err := m.DB(ctx).Where("group_key = ?", groupKey).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (m *Model) ListByStatus(ctx context.Context, status int) (rows []SysConfigEntity, err error) {
	q := m.DB(ctx)
	if status >= 0 {
		q = q.Where("status = ?", status)
	}
	err = q.Order("group_key ASC").Find(&rows).Error
	return rows, err
}

func (m *Model) UpdateDataWithVersion(ctx context.Context, groupKey string, version int64, data JSONMap, updateBy int64) (int64, error) {
	res := m.DB(ctx).
		Where("group_key = ? AND version = ?", groupKey, version).
		Updates(map[string]any{
			"config_data": data,
			"version":     gorm.Expr("version + 1"),
			"update_by":   updateBy,
			"update_time": gorm.Expr("now()"),
		})
	return res.RowsAffected, res.Error
}

func (m *Model) VersionOf(ctx context.Context, groupKey string) (version int64, found bool, err error) {
	var row struct {
		Version int64 `gorm:"column:version"`
	}
	terr := m.DB(ctx).Select("version").Where("group_key = ?", groupKey).Take(&row).Error
	if errors.Is(terr, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if terr != nil {
		return 0, false, terr
	}
	return row.Version, true, nil
}

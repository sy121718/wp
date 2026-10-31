package pluginmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type Entity struct {
	PluginID      string    `gorm:"column:plugin_id;primaryKey"`
	Name          string    `gorm:"column:name"`
	Version       string    `gorm:"column:version"`
	SchemaVersion int       `gorm:"column:schema_version"`
	Enabled       bool      `gorm:"column:enabled"`
	Manifest      []byte    `gorm:"column:manifest"`
	StoragePath   string    `gorm:"column:storage_path"`
	InstalledAt   time.Time `gorm:"column:installed_at"`
	UpdatedAt     time.Time `gorm:"column:update_time"`
}

func (Entity) TableName() string { return "plugin_registry" }

type Model struct {
	db *gorm.DB
}

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&Entity{})
}

func (m *Model) Get(ctx context.Context, pluginID string) (e *Entity, err error) {
	var row Entity
	if err = m.db.WithContext(ctx).Where("plugin_id = ?", pluginID).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) List(ctx context.Context) (list []*Entity, err error) {
	err = m.db.WithContext(ctx).Order("installed_at DESC").Find(&list).Error
	return list, err
}

func (m *Model) ListEnabled(ctx context.Context) (list []*Entity, err error) {
	err = m.db.WithContext(ctx).Where("enabled = ?", true).
		Order("plugin_id ASC").Find(&list).Error
	return list, err
}

type EnabledFingerprintRow struct {
	PluginID    string    `gorm:"column:plugin_id"`
	Version     string    `gorm:"column:version"`
	StoragePath string    `gorm:"column:storage_path"`
	UpdatedAt   time.Time `gorm:"column:update_time"`
}

func (m *Model) ListEnabledFingerprint(ctx context.Context) (list []EnabledFingerprintRow, err error) {
	err = m.db.WithContext(ctx).Model(&Entity{}).
		Select("plugin_id, version, storage_path, update_time").
		Where("enabled = ?", true).
		Order("plugin_id ASC").
		Find(&list).Error
	return list, err
}

func (m *Model) Create(ctx context.Context, e *Entity) error {
	return m.db.WithContext(ctx).Create(e).Error
}

func (m *Model) Update(ctx context.Context, e *Entity) error {
	return m.db.WithContext(ctx).Save(e).Error
}

func (m *Model) Delete(ctx context.Context, pluginID string) error {
	return m.db.WithContext(ctx).Where("plugin_id = ?", pluginID).Delete(&Entity{}).Error
}

func (m *Model) Exec(ctx context.Context, sql string) error {
	return m.db.WithContext(ctx).Exec(sql).Error
}

func (m *Model) ExecTx(ctx context.Context, tx *gorm.DB, sql string) error {
	return tx.WithContext(ctx).Exec(sql).Error
}

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

type SchemaInfo struct {
	Name       string `gorm:"column:schema_name"`
	TableCount int    `gorm:"column:table_count"`
}

func (m *Model) ListSchemas(ctx context.Context) (list []SchemaInfo, err error) {
	err = m.db.WithContext(ctx).Raw(
		`SELECT n.nspname AS schema_name, COUNT(c.oid)::int AS table_count
		   FROM pg_namespace n
		   LEFT JOIN pg_class c ON c.relnamespace = n.oid AND c.relkind = 'r'
		  WHERE left(n.nspname, length(?)) = ?
		  GROUP BY n.nspname
		  ORDER BY n.nspname`, schemaPrefix, schemaPrefix).Scan(&list).Error
	return list, err
}

const schemaPrefix = "plugin_"

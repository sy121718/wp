// Package pluginmodel 插件注册表的表访问单元（Repository，plugin_registry 表）。
package pluginmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// Entity plugin_registry 表实体。
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

// TableName 表名。
func (Entity) TableName() string { return "plugin_registry" }

// Model 插件注册表访问。
type Model struct {
	db *gorm.DB
}

// NewModel 构造。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 绑定本表的查询入口（WithContext）。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&Entity{})
}

// Get 按插件 ID 查询。
func (m *Model) Get(ctx context.Context, pluginID string) (e *Entity, err error) {
	var row Entity
	if err = m.db.WithContext(ctx).Where("plugin_id = ?", pluginID).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// List 全部插件（安装时间倒序）。
func (m *Model) List(ctx context.Context) (list []*Entity, err error) {
	err = m.db.WithContext(ctx).Order("installed_at DESC").Find(&list).Error
	return list, err
}

// ListEnabled 全部启用插件（plugin_id 字典序，确定性供编译装配）。
func (m *Model) ListEnabled(ctx context.Context) (list []*Entity, err error) {
	err = m.db.WithContext(ctx).Where("enabled = ?", true).
		Order("plugin_id ASC").Find(&list).Error
	return list, err
}

// EnabledFingerprintRow 启用插件集的指纹行（审计 PERF-006）。
//
// 只取「装配素材是否变化」相关的列：换插件、换版本、重装（update_time 变）都会改指纹。
// Manifest 与其它大字段不在其中 —— 命中缓存时连它们都不必从数据库取回。
type EnabledFingerprintRow struct {
	PluginID    string    `gorm:"column:plugin_id"`
	Version     string    `gorm:"column:version"`
	StoragePath string    `gorm:"column:storage_path"`
	UpdatedAt   time.Time `gorm:"column:update_time"`
}

// ListEnabledFingerprint 启用插件集的轻量指纹查询（列投影，不含 manifest 字节）。
func (m *Model) ListEnabledFingerprint(ctx context.Context) (list []EnabledFingerprintRow, err error) {
	err = m.db.WithContext(ctx).Model(&Entity{}).
		Select("plugin_id, version, storage_path, update_time").
		Where("enabled = ?", true).
		Order("plugin_id ASC").
		Find(&list).Error
	return list, err
}

// Create 插入注册行。
func (m *Model) Create(ctx context.Context, e *Entity) error {
	return m.db.WithContext(ctx).Create(e).Error
}

// Update 覆盖更新（安装/升级/启停）。
func (m *Model) Update(ctx context.Context, e *Entity) error {
	return m.db.WithContext(ctx).Save(e).Error
}

// Delete 按插件 ID 删除。
func (m *Model) Delete(ctx context.Context, pluginID string) error {
	return m.db.WithContext(ctx).Where("plugin_id = ?", pluginID).Delete(&Entity{}).Error
}

// Exec 在底层裸连接上执行一条 SQL（供 L1 数据层迁移执行器跑 DDL/SCHEMA 语句）。
func (m *Model) Exec(ctx context.Context, sql string) error {
	return m.db.WithContext(ctx).Exec(sql).Error
}

// Transaction 在底层裸连接上开启事务（供 L1 迁移执行器整包执行、失败回滚）。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

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

// ExecTx 在**调用方已开启的事务句柄**上执行一条 SQL（L1 数据层迁移执行器的整包事务）。
//
// 与 Exec 同源同义：跑的是插件自带的 DDL / SCHEMA 语句（不是 plugin_registry 的行访问），
// migrateSchema 把「清理旧 schema → 锁 search_path → 逐条执行迁移文件」放在一个事务里，
// 任一条失败整体回滚。执行口只有这一个 —— 非事务路径走 Exec、事务路径走 ExecTx，
// 句柄由 service 透传，避免「事务里直接拼 tx.Exec」与 model 的两套写法并存。
func (m *Model) ExecTx(ctx context.Context, tx *gorm.DB, sql string) error {
	return tx.WithContext(ctx).Exec(sql).Error
}

// Transaction 在底层裸连接上开启事务（供 L1 迁移执行器整包执行、失败回滚）。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// SchemaInfo 一个插件 L1 schema 的巡检条目（plugin_schema_patrol.go 使用）。
//
// TableCount = 这个 schema 里有几张普通表。它是给人做判断用的：空 schema 可以判为
// 「卸载没清干净」，有表的孤儿 schema 里可能是真实业务数据 —— 必须先看清再决定，
// 不能因为「注册表里没有」就当它不存在。
type SchemaInfo struct {
	Name       string `gorm:"column:schema_name"`
	TableCount int    `gorm:"column:table_count"`
}

// ListSchemas 列出库里全部 plugin_ 前缀 schema 及其表数量（只读巡检）。
//
// 为什么查 PG 目录而不是本模块的注册表：**只有绕过注册表直接问数据库，才看得见
// 「注册表里没有、数据库里还在」的孤儿 schema** —— 那正是要巡检的东西。
// 这是本 model 唯一一处不落在 plugin_registry 上的查询，用途限定为对账；
// 它不写任何数据，也不参与业务路径。
//
// 用 left(nspname, 7) 而不是 LIKE 'plugin_%'：LIKE 里下划线是单字符通配符，
// 'plugin_%' 会把 pluginXxx 之类的 schema 一起匹进来（要转义才等价，容易写错）。
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

// schemaPrefix 与 service 层的 schemaNameFor 同源（plugin_<id>，docs/06 §8）。
// 在 model 里重声明一份常量而不是 import service：model 不能依赖 service（依赖方向是
// service → model），而巡检的 SQL 需要这个前缀。两处一致由
// plugin/service 的 plugin_schema_patrol_test.go 钉住。
const schemaPrefix = "plugin_"

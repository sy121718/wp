package unit

// 插件 L1 数据层迁移执行器 feature 测试（真实 PostgreSQL）：
// 安装携带 migrations 的插件 → schema 创建 → 卸载 → schema 级联删除；
// 纯展示插件（无 migrations）不建 schema；同版本重装幂等跳过。

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"testing"

	plugindto "go_wp/internal/module/plugin/dto"
	pluginmodel "go_wp/internal/module/plugin/model"
	pluginservice "go_wp/internal/module/plugin/service"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

// newMigrateService 隔离 PG schema + AutoMigrate plugin_registry + 装配 service，
// 同时返回裸 *gorm.DB 供断言 schema 存在性。
func newMigrateService(t *testing.T) (*gorm.DB, *pluginservice.Service) {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil, nil
	}
	if err := db.AutoMigrate(&pluginmodel.Entity{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	return db, pluginservice.NewService(pluginmodel.NewModel(db))
}

// migrationManifest 带 L1 迁移声明的 manifest（migrations 目录 + schemaVersion=1）。
func migrationManifest(id string) string {
	return fmt.Sprintf(`{
  "id": "%s",
  "name": "营销系统",
  "version": "1.0.0",
  "migrations": "migrations",
  "schemaVersion": 1,
  "components": [{
    "name": "campaign_card",
    "label": "活动卡片",
    "template": "campaign_card.jet",
    "props": {"title": {"kind": "text", "label": "标题", "default": "促销"}}
  }]
}`, id)
}

// pluginZipWithMigrations 构造含 migrations 目录 + 建表 SQL 的插件 zip。
// 迁移 SQL 按 docs/06 §8 约定自行包含 CREATE SCHEMA IF NOT EXISTS + 建表。
func pluginZipWithMigrations(t *testing.T, manifest, schemaName string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"manifest.json":                manifest,
		"components/campaign_card.jet": "<article class='{{ .Classes }}'>{{ .V.title }}</article>",
		"migrations/001_init.sql": fmt.Sprintf(
			"CREATE SCHEMA IF NOT EXISTS %s;\nCREATE TABLE IF NOT EXISTS %s.campaigns (id serial PRIMARY KEY, title text NOT NULL);\n",
			schemaName, schemaName),
	}
	for name, content := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(content))
	}
	_ = zw.Close()
	return buf.Bytes()
}

// schemaExists 查询 information_schema 判断 schema 是否存在。
func schemaExists(t *testing.T, db *gorm.DB, schemaName string) bool {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?", schemaName).Scan(&count).Error; err != nil {
		t.Fatalf("查询 schema 失败: %v", err)
	}
	return count > 0
}

// TestPluginMigrateInstallUninstall 安装建 schema → 卸载级联 drop。
func TestPluginMigrateInstallUninstall(t *testing.T) {
	db, svc := newMigrateService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	const id = "marketing"
	schemaName := "plugin_" + id
	// 防御性清理：失败中断时也不残留 schema。
	t.Cleanup(func() { _ = db.Exec(`DROP SCHEMA IF EXISTS "` + schemaName + `" CASCADE`).Error })

	z := pluginZipWithMigrations(t, migrationManifest(id), schemaName)
	if _, err := svc.Install(ctx, z); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	if !schemaExists(t, db, schemaName) {
		t.Fatalf("安装后 schema %s 应存在", schemaName)
	}
	// 表也应已建好。
	var tbl int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = 'campaigns'", schemaName).Scan(&tbl).Error; err != nil {
		t.Fatalf("查询表失败: %v", err)
	}
	if tbl == 0 {
		t.Fatalf("安装后 %s.campaigns 应存在", schemaName)
	}
	// 卸载 → schema 级联删除。
	if err := svc.Uninstall(ctx, &plugindto.UninstallReq{ID: id}); err != nil {
		t.Fatalf("卸载失败: %v", err)
	}
	if schemaExists(t, db, schemaName) {
		t.Fatalf("卸载后 schema %s 应已删除", schemaName)
	}
}

// TestPluginMigratePureDisplay 纯展示插件（无 migrations）不建 schema。
func TestPluginMigratePureDisplay(t *testing.T) {
	db, svc := newMigrateService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	const id = "marketing"
	schemaName := "plugin_" + id
	t.Cleanup(func() { _ = db.Exec(`DROP SCHEMA IF EXISTS "` + schemaName + `" CASCADE`).Error })

	if _, err := svc.Install(ctx, pluginZip(t, testManifest)); err != nil {
		t.Fatalf("纯展示插件安装失败: %v", err)
	}
	if schemaExists(t, db, schemaName) {
		t.Fatalf("纯展示插件不应创建 schema %s", schemaName)
	}
}

// TestPluginMigrateIdempotentReinstall 同版本重装幂等：跳过迁移，schema 保留。
func TestPluginMigrateIdempotentReinstall(t *testing.T) {
	db, svc := newMigrateService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	const id = "marketing"
	schemaName := "plugin_" + id
	t.Cleanup(func() { _ = db.Exec(`DROP SCHEMA IF EXISTS "` + schemaName + `" CASCADE`).Error })

	z := pluginZipWithMigrations(t, migrationManifest(id), schemaName)
	if _, err := svc.Install(ctx, z); err != nil {
		t.Fatalf("首次安装失败: %v", err)
	}
	// 第二次安装同版本：应成功且 schema 仍在（幂等跳过迁移）。
	if _, err := svc.Install(ctx, z); err != nil {
		t.Fatalf("同版本重装失败: %v", err)
	}
	if !schemaExists(t, db, schemaName) {
		t.Fatalf("同版本重装后 schema %s 应保留", schemaName)
	}
}

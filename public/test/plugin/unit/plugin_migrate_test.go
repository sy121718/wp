package unit

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

func newMigrateService(t *testing.T) (*gorm.DB, *pluginservice.Service) {
	t.Helper()

	t.Setenv("GO_WP_PLUGIN_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	grantPluginMigrationCreate(t, db)
	return db, pluginservice.NewService(pluginmodel.NewModel(db))
}

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

func schemaExists(t *testing.T, db *gorm.DB, schemaName string) bool {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = ?", schemaName).Scan(&count).Error; err != nil {
		t.Fatalf("查询 schema 失败: %v", err)
	}
	return count > 0
}

func TestPluginMigrateInstallUninstall(t *testing.T) {
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
		t.Fatalf("安装失败: %v", err)
	}
	if !schemaExists(t, db, schemaName) {
		t.Fatalf("安装后 schema %s 应存在", schemaName)
	}

	var tbl int64
	if err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = 'campaigns'", schemaName).Scan(&tbl).Error; err != nil {
		t.Fatalf("查询表失败: %v", err)
	}
	if tbl == 0 {
		t.Fatalf("安装后 %s.campaigns 应存在", schemaName)
	}

	if err := svc.Uninstall(ctx, &plugindto.UninstallReq{ID: id}); err != nil {
		t.Fatalf("卸载失败: %v", err)
	}
	if schemaExists(t, db, schemaName) {
		t.Fatalf("卸载后 schema %s 应已删除", schemaName)
	}
}

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

	if _, err := svc.Install(ctx, z); err != nil {
		t.Fatalf("同版本重装失败: %v", err)
	}
	if !schemaExists(t, db, schemaName) {
		t.Fatalf("同版本重装后 schema %s 应保留", schemaName)
	}
}

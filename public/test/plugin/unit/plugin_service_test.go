package unit

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginmodel "go_wp/internal/module/plugin/model"
	pluginservice "go_wp/internal/module/plugin/service"

	plugindto "go_wp/internal/module/plugin/dto"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

func pluginZip(t *testing.T, manifest string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"manifest.json":                manifest,
		"components/campaign_card.jet": "<article class='{{ .Classes }}'>{{ .V.title }}</article>",
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(content))
	}
	_ = zw.Close()
	return buf.Bytes()
}

const testManifest = `{
  "id": "marketing",
  "name": "营销组件",
  "version": "1.0.0",
  "components": [{
    "name": "campaign_card",
    "label": "活动卡片",
    "template": "campaign_card.jet",
    "props": {"title": {"kind": "text", "label": "标题", "default": "促销"}}
  }]
}`

func newService(t *testing.T) *pluginservice.Service {
	t.Helper()

	t.Setenv("GO_WP_PLUGIN_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	grantPluginMigrationCreate(t, db)
	return pluginservice.NewService(pluginmodel.NewModel(db))
}

func grantPluginMigrationCreate(t *testing.T, db *gorm.DB) {
	t.Helper()
	quoteIdent := func(name string) string {
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
	databaseName := quoteIdent(db.Migrator().CurrentDatabase())
	roleName := quoteIdent(pluginservice.PluginMigrationRole)

	if err := db.Exec("GRANT CREATE ON DATABASE " + databaseName + " TO " + roleName).Error; err != nil {
		t.Skipf("无法在测试库上授权（插件迁移需要该前提）: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("REVOKE CREATE ON DATABASE " + databaseName + " FROM " + roleName).Error
	})
}

func TestInstallListToggleUninstall(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	if _, err := svc.Install(ctx, pluginZip(t, testManifest)); err != nil {
		t.Fatalf("安装失败: %v", err)
	}

	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("列表失败: %v len=%d", err, len(list))
	}
	if list[0].ID != "marketing" || list[0].Version != "1.0.0" || !list[0].Enabled {
		t.Fatalf("列表行错误: %+v", list[0])
	}
	if list[0].ComponentCount != 1 {
		t.Fatalf("组件数应为 1: %d", list[0].ComponentCount)
	}

	asm, err := svc.EnabledAssembly(ctx)
	if err != nil || len(asm.Specs) != 1 {
		t.Fatalf("装配素材错误: %v specs=%d", err, len(asm.Specs))
	}
	if _, ok := asm.Specs["plugin.marketing.campaign_card"]; !ok {
		t.Fatalf("缺组件规格")
	}

	if err := svc.Toggle(ctx, &plugindto.ToggleReq{ID: "marketing", Enabled: false}); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	asmOff, _ := svc.EnabledAssembly(ctx)
	if len(asmOff.Specs) != 0 {
		t.Fatalf("停用后装配素材应为空")
	}

	if err := svc.Uninstall(ctx, &plugindto.UninstallReq{ID: "marketing"}); err != nil {
		t.Fatalf("卸载失败: %v", err)
	}
	list2, _ := svc.List(ctx)
	if len(list2) != 0 {
		t.Fatalf("卸载后列表应为空")
	}
}

func TestInstallRejectsBadPackage(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	if _, err := svc.Install(ctx, pluginZip(t, `{"id":"x","name":"x","version":"1.0.0","components":[]}`)); err == nil {
		t.Fatalf("无组件 manifest 应拒绝")
	}

	bad := `{"id":"X","name":"x","version":"1.0.0","components":[{"name":"c","label":"c","template":"c.jet"}]}`
	if _, err := svc.Install(ctx, pluginZip(t, bad)); err == nil {
		t.Fatalf("非法 manifest 应拒绝")
	}

	if _, err := svc.Install(ctx, nil); err == nil {
		t.Fatalf("空包应拒绝")
	}
}

// TestInstallRollsBackSchemaWhenStorageFails 覆盖「schema 已建、文件没写成」：
// 安装失败必须把刚建的 schema 撤掉，否则下一次安装会撞上残留。
func TestInstallRollsBackSchemaWhenStorageFails(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	// 把插件根目录指到一个文件上：MkdirAll 必然失败，模拟磁盘写失败。
	root := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_WP_PLUGIN_ROOT", root)

	manifest := `{
  "id": "rollback_probe",
  "name": "回滚探针",
  "version": "1.0.0",
  "migrations": "migrations",
  "components": [{"name":"probe","label":"探针","template":"probe.jet"}]
}`
	zipBytes := zipWithFiles(t, map[string]string{
		"manifest.json":            manifest,
		"components/probe.jet":     "<div>{{ .Classes }}</div>",
		"migrations/001_init.sql":  "CREATE TABLE IF NOT EXISTS probe (id int);",
	})
	if _, err := svc.Install(ctx, zipBytes); err == nil {
		t.Fatal("存储失败时安装必须报错")
	}

	// schema 必须已回滚：再装一次同 id 的包不应撞上残留（这里只验 schema 不存在）。
	db := support.NewMigratedPGTestDB(t)
	var exists bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = ?)",
		"plugin_rollback_probe").Scan(&exists).Error; err != nil {
		t.Fatalf("查询 schema 失败: %v", err)
	}
	if exists {
		t.Fatal("安装失败后插件 schema 必须已被回滚")
	}
}

// zipWithFiles 按给定文件表打包（插件包的最小形态）。
func zipWithFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

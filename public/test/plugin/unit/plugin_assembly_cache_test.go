package unit

import (
	"context"
	"testing"

	plugindto "go_wp/internal/module/plugin/dto"
	"go_wp/internal/templates"

	"gorm.io/gorm"
)

func insertRegistryRow(t *testing.T, db *gorm.DB, id, version, dir, manifest string, enabled bool) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO plugin_registry (plugin_id, name, version, schema_version, enabled, manifest, storage_path, installed_at, update_time)
		 VALUES (?, ?, ?, 1, ?, ?::jsonb, ?, now(), now())`,
		id, id, version, enabled, manifest, dir).Error; err != nil {
		t.Fatalf("插入插件注册行失败: %v", err)
	}
}

func TestEnabledAssemblyCacheReuseAndInvalidate(t *testing.T) {
	db, svc := newMigrateService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	dir := t.TempDir()
	insertRegistryRow(t, db, "marketing", "1.0.0", dir, migrationManifest("marketing"), true)

	first, err := svc.EnabledAssembly(ctx)
	if err != nil {
		t.Fatalf("首次装配失败: %v", err)
	}
	if first.Fingerprint == "" {
		t.Fatal("装配素材必须带启用集指纹，否则下游无法复用 Jet Set")
	}
	if len(first.PluginFS) != 1 {
		t.Fatalf("应装配 1 个插件的模板 FS: %d", len(first.PluginFS))
	}

	second, err := svc.EnabledAssembly(ctx)
	if err != nil {
		t.Fatalf("二次装配失败: %v", err)
	}
	if first != second {
		t.Fatal("同一启用集应命中缓存并返回同一份装配素材")
	}

	if err = svc.Toggle(ctx, &plugindto.ToggleReq{ID: "marketing", Enabled: false}); err != nil {
		t.Fatalf("停用插件失败: %v", err)
	}
	third, err := svc.EnabledAssembly(ctx)
	if err != nil {
		t.Fatalf("停用后装配失败: %v", err)
	}
	if third == first {
		t.Fatal("启用集变化后必须重建装配素材（缓存未失效）")
	}
	if third.Fingerprint == first.Fingerprint {
		t.Fatal("启用集变化必须改变指纹")
	}
	if len(third.PluginFS) != 0 {
		t.Fatalf("停用后不应再有插件模板: %d", len(third.PluginFS))
	}
}

func TestCompositeSetCacheReusesByFingerprint(t *testing.T) {
	templates.ResetCompositeSetCache()
	t.Cleanup(templates.ResetCompositeSetCache)

	a, err := templates.NewCompositeSetCached("fp-a", nil)
	if err != nil {
		t.Fatalf("构建 Set 失败: %v", err)
	}
	b, err := templates.NewCompositeSetCached("fp-a", nil)
	if err != nil {
		t.Fatalf("复用构建失败: %v", err)
	}
	if a != b {
		t.Fatal("同指纹应复用同一个 Jet Set")
	}
	c, err := templates.NewCompositeSetCached("fp-b", nil)
	if err != nil {
		t.Fatalf("构建 Set 失败: %v", err)
	}
	if c == a {
		t.Fatal("不同指纹不得复用同一个 Set")
	}

	d, err := templates.NewCompositeSetCached("", nil)
	if err != nil {
		t.Fatalf("空指纹构建失败: %v", err)
	}
	e, err := templates.NewCompositeSetCached("", nil)
	if err != nil {
		t.Fatalf("空指纹构建失败: %v", err)
	}
	if d == e {
		t.Fatal("空指纹必须退化为每次新建，不得共享缓存槽")
	}
}

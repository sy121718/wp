package unit

import (
	"context"
	"testing"

	plugindto "go_wp/internal/module/plugin/dto"
	"go_wp/internal/templates"

	"gorm.io/gorm"
)

// 审计 PERF-006：启用插件时每次页面编译都重查数据库、读磁盘并新建 Jet Set。
// 本文件固化缓存的两条语义：同一启用集复用同一份装配；启用集一变立刻重建。

// insertRegistryRow 直插插件注册行（装配只关心注册行 + 磁盘目录，不必走 zip 安装）。
func insertRegistryRow(t *testing.T, db *gorm.DB, id, version, dir, manifest string, enabled bool) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO plugin_registry (plugin_id, name, version, schema_version, enabled, manifest, storage_path, installed_at, update_time)
		 VALUES (?, ?, ?, 1, ?, ?::jsonb, ?, now(), now())`,
		id, id, version, enabled, manifest, dir).Error; err != nil {
		t.Fatalf("插入插件注册行失败: %v", err)
	}
}

// TestEnabledAssemblyCacheReuseAndInvalidate 同启用集复用、启用集变化重建。
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

	// 启停是启用集的变化：必须立即反映，不能等缓存过期（本缓存没有过期时间）。
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

// TestCompositeSetCacheReusesByFingerprint Jet Set 按指纹复用，空指纹不共享槽位。
//
// 空指纹单列一条断言：指纹缺失时若也走同一个缓存槽，两套不同的插件集会互相
// 复用模板（装上插件却渲染出上一套的模板），这是比「没缓存」严重得多的失败。
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

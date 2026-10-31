package unit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPatrolArtifactsAgainstRealStores(t *testing.T) {
	db, svc := newMigrateService(t)
	ctx := context.Background()

	root := os.Getenv("GO_WP_PLUGIN_ROOT")
	if root == "" {
		t.Fatal("测试应已隔离 GO_WP_PLUGIN_ROOT")
	}

	base, err := svc.PatrolArtifacts(ctx)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	if len(base.OrphanSchemas) != 0 || len(base.MissingSchemas) != 0 ||
		len(base.OrphanStorage) != 0 || len(base.MissingStorage) != 0 {
		t.Fatalf("基线不该有任何不一致：%+v", base)
	}
	if base.StorageRoot == "" {
		t.Fatalf("巡检应给出存储根路径（页面要拼出可操作的路径）")
	}

	if err := db.Exec("CREATE SCHEMA plugin_orphan_probe").Error; err != nil {
		t.Fatalf("建探针 schema 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS plugin_orphan_probe CASCADE").Error })
	if err := db.Exec("CREATE TABLE plugin_orphan_probe.t1 (id int)").Error; err != nil {
		t.Fatalf("建探针表失败: %v", err)
	}

	if err := db.Exec("CREATE SCHEMA pluginx_probe").Error; err != nil {
		t.Fatalf("建陷阱 schema 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS pluginx_probe CASCADE").Error })

	if err := os.MkdirAll(filepath.Join(root, "leftover"), 0o755); err != nil {
		t.Fatalf("建孤儿目录失败: %v", err)
	}

	patrol, err := svc.PatrolArtifacts(ctx)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	var tableCount *int
	for i := range patrol.OrphanSchemas {
		switch patrol.OrphanSchemas[i].Name {
		case "plugin_orphan_probe":
			n := patrol.OrphanSchemas[i].TableCount
			tableCount = &n
		case "pluginx_probe":
			t.Fatalf("pluginx_probe 不是 plugin_ 前缀，不该被当成插件 schema 报出来")
		}
	}
	if tableCount == nil {
		t.Fatalf("应报出孤儿 schema plugin_orphan_probe，实际 %+v", patrol.OrphanSchemas)
	}
	if *tableCount != 1 {
		t.Fatalf("孤儿 schema 的表数量应为 1，实际 %d", *tableCount)
	}
	if len(patrol.OrphanStorage) != 1 || patrol.OrphanStorage[0] != "leftover" {
		t.Fatalf("应报出孤儿目录 leftover，实际 %v", patrol.OrphanStorage)
	}

	ghostDir := t.TempDir()
	if err := db.Exec(`INSERT INTO plugin_registry
		(plugin_id, name, version, schema_version, enabled, manifest, storage_path, installed_at, update_time)
		VALUES (?, ?, ?, ?, ?, '{}'::jsonb, ?, now(), now())`,
		"ghost", "幽灵插件", "1.0.0", 1, true, ghostDir).Error; err != nil {
		t.Fatalf("插注册行失败: %v", err)
	}

	if err := db.Exec(`INSERT INTO plugin_registry
		(plugin_id, name, version, schema_version, enabled, manifest, storage_path, installed_at, update_time)
		VALUES (?, ?, ?, ?, ?, '{}'::jsonb, ?, now(), now())`,
		"pure", "纯组件插件", "1.0.0", 0, true, t.TempDir()).Error; err != nil {
		t.Fatalf("插注册行失败: %v", err)
	}

	if err := db.Exec(`INSERT INTO plugin_registry
		(plugin_id, name, version, schema_version, enabled, manifest, storage_path, installed_at, update_time)
		VALUES (?, ?, ?, ?, ?, '{}'::jsonb, ?, now(), now())`,
		"nodir", "缺目录插件", "1.0.0", 1, true, filepath.Join(root, "does-not-exist")).Error; err != nil {
		t.Fatalf("插注册行失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM plugin_registry WHERE plugin_id IN (?,?,?)", "ghost", "pure", "nodir").Error
	})

	patrol, err = svc.PatrolArtifacts(ctx)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	missing := map[string]bool{}
	for _, id := range patrol.MissingSchemas {
		missing[id] = true
	}
	if !missing["ghost"] {
		t.Fatalf("ghost 声明了 schema_version=1 但 schema 不存在，应被报出，实际 %v", patrol.MissingSchemas)
	}
	if missing["pure"] {
		t.Fatalf("schema_version=0 的插件不该被算成缺 schema，实际 %v", patrol.MissingSchemas)
	}

	if !missing["nodir"] {
		t.Fatalf("nodir 也没有 schema，应同时被报出，实际 %v", patrol.MissingSchemas)
	}
	dirMissing := map[string]bool{}
	for _, id := range patrol.MissingStorage {
		dirMissing[id] = true
	}
	if !dirMissing["nodir"] {
		t.Fatalf("nodir 的存储目录不存在，应被报出，实际 %v", patrol.MissingStorage)
	}
	if dirMissing["ghost"] || dirMissing["pure"] {
		t.Fatalf("StoragePath 指向真实存在的目录时不该报「目录缺失」，实际 %v", patrol.MissingStorage)
	}

	if len(patrol.OrphanStorage) != 1 || patrol.OrphanStorage[0] != "leftover" {
		t.Fatalf("注册之后 leftover 仍应是唯一孤儿目录，实际 %v", patrol.OrphanStorage)
	}
}

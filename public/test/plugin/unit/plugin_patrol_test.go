package unit

// 插件「三处产物」对账巡检的 feature 测试（真实 PostgreSQL + 真实文件系统）。
//
// 纯函数单测（plugin/service/plugin_patrol_test.go）覆盖「分类」那一步；
// 真正容易静默写错的是**两侧的采集**：
//   · 数据库侧：pg_namespace / pg_class 是**全库**的，前缀判据写错（比如用
//     LIKE 'plugin_%' —— 下划线是单字符通配符）会把 pluginXxx 一起卷进插件域，
//     而结果看起来完全合理（多出几行「孤儿」而已）；
//   · 文件系统侧：只在目录不存在时返回空而不是报错、只认目录不认文件。
// 只有真库 + 真目录才能证伪，所以这里两者都动。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// 注册行夹具用显式 SQL 而不是 model.Create：要写的列一目了然；
// 走 model 反而会引入「manifest 是 []byte 还是 jsonb」这种与判据无关的噪音。

// TestPatrolArtifactsAgainstRealStores 巡检对齐真实目录与真实磁盘。
func TestPatrolArtifactsAgainstRealStores(t *testing.T) {
	db, svc := newMigrateService(t)
	ctx := context.Background()
	// newMigrateService 把插件存储根隔离到临时目录（t.Setenv），这里复用它来造目录。
	root := os.Getenv("GO_WP_PLUGIN_ROOT")
	if root == "" {
		t.Fatal("测试应已隔离 GO_WP_PLUGIN_ROOT")
	}

	// 基线：测试库按模板库复制，模板里没有任何 plugin_ 前缀 schema，存储根也是空目录。
	// 这里不为空就说明别处泄漏了状态（或判据把无关对象卷了进来）—— 必须在第一条断言暴露。
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

	// ① 孤儿 schema：schema 在、注册行不在，里面有一张表 → 表数量必须报对。
	if err := db.Exec("CREATE SCHEMA plugin_orphan_probe").Error; err != nil {
		t.Fatalf("建探针 schema 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS plugin_orphan_probe CASCADE").Error })
	if err := db.Exec("CREATE TABLE plugin_orphan_probe.t1 (id int)").Error; err != nil {
		t.Fatalf("建探针表失败: %v", err)
	}

	// ② 前缀陷阱：pluginx_probe 第 7 个字符是 x 不是下划线，不属于插件域。
	if err := db.Exec("CREATE SCHEMA pluginx_probe").Error; err != nil {
		t.Fatalf("建陷阱 schema 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS pluginx_probe CASCADE").Error })

	// ③ 孤儿存储目录：根下有个目录、注册表里没有对应 ID。
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

	// ④ 缺 schema：声明 schema_version > 0 但 schema 不在。
	//    给一条真实存在的 StoragePath，避免同时触发「目录缺失」把这条断言弄脏。
	ghostDir := t.TempDir()
	if err := db.Exec(`INSERT INTO plugin_registry
		(plugin_id, name, version, schema_version, enabled, manifest, storage_path, installed_at, update_time)
		VALUES (?, ?, ?, ?, ?, '{}'::jsonb, ?, now(), now())`,
		"ghost", "幽灵插件", "1.0.0", 1, true, ghostDir).Error; err != nil {
		t.Fatalf("插注册行失败: %v", err)
	}
	// ⑤ schema_version = 0 的纯组件插件按设计没有 L1 schema，不该被算成「缺 schema」。
	if err := db.Exec(`INSERT INTO plugin_registry
		(plugin_id, name, version, schema_version, enabled, manifest, storage_path, installed_at, update_time)
		VALUES (?, ?, ?, ?, ?, '{}'::jsonb, ?, now(), now())`,
		"pure", "纯组件插件", "1.0.0", 0, true, t.TempDir()).Error; err != nil {
		t.Fatalf("插注册行失败: %v", err)
	}
	// ⑥ 目录缺失：注册行在、StoragePath 指向不存在的路径。
	if err := db.Exec(`INSERT INTO plugin_registry
		(plugin_id, name, version, schema_version, enabled, manifest, storage_path, installed_at, update_time)
		VALUES (?, ?, ?, ?, ?, '{}'::jsonb, ?, now(), now())`,
		"nodir", "缺目录插件", "1.0.0", 1, true, filepath.Join(root, "does-not-exist")).Error; err != nil {
		t.Fatalf("插注册行失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DELETE FROM plugin_registry WHERE plugin_id IN (?,?,?)", "ghost", "pure", "nodir").Error })

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
	// nodir 也没有 schema，所以它同时出现在两类里 —— 这是正确的：
	// 两处不一致各自都要人处置（它既没有 schema 也没有目录）。
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
	// 现在 ghost / pure / nodir 都已注册，leftover 仍是孤儿目录；plugin_orphan_probe 仍是孤儿 schema。
	if len(patrol.OrphanStorage) != 1 || patrol.OrphanStorage[0] != "leftover" {
		t.Fatalf("注册之后 leftover 仍应是唯一孤儿目录，实际 %v", patrol.OrphanStorage)
	}
}

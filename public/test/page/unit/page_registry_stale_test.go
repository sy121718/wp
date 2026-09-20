package unit

// page_registry_stale_test.go — 组件升级后的 stale 判定只看「当前产物」（报告 ARCH-03）。
//
// 判据：当前产物 = 本模块语言账本（page_publications / page_stagings）里 active/staged
// 指向的产物行。历史回滚产物（仍 available、未被 GC）不参与判定 —— 旧实现扫全部 available
// 行，未 GC 的旧行会让「已重建」的页面每次重启都被重新标成 stale，且每次失败的包都不同。

import (
	"context"
	"testing"

	"go_wp/internal/builder"

	"gorm.io/gorm"
)

// insertRegistryArtifact 造一行可控 registry_version 的产物元数据。
//
// 真实构建路径写的是当前 builder.RegistryVersion()，造不出「旧组件产出」那一侧；
// 这里只补产物行本身（表结构仍来自生产迁移）。
func insertRegistryArtifact(t *testing.T, db *gorm.DB, pageID, lang string, version int64, registryVersion string) string {
	t.Helper()
	var id string
	if err := db.Raw(`INSERT INTO page_artifacts (
		id, page_id, version, source_document, page_document_schema_version, source_hash,
		build_input_manifest, build_input_hash, artifact_provider, artifact_key, artifact_hash,
		compiler_version, registry_version, manifest, payload_state, note, created_by, create_time, lang
	) VALUES (gen_random_uuid(), ?, ?, '{}'::jsonb, 1, 'src', '{}'::jsonb, 'bih', 'local', ?, ?,
		'v1', ?, '{}'::jsonb, 'available', '', gen_random_uuid(), now(), ?)
	RETURNING id`,
		pageID, version, "key-"+lang+"-"+registryVersion, "hash-"+lang+"-"+registryVersion,
		registryVersion, lang).Scan(&id).Error; err != nil {
		t.Fatalf("插入产物行失败: %v", err)
	}
	return id
}

// pointPageLedger 把某语言的语言账本（激活 + 暂存）指向给定产物行。
func pointPageLedger(t *testing.T, db *gorm.DB, pageID, lang, artifactID string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO page_publications (page_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time)
		VALUES (?, ?, ?, ?, 'h', now(), now())
		ON CONFLICT (page_id, lang) DO UPDATE SET artifact_id = EXCLUDED.artifact_id, update_time = now()`,
		pageID, lang, "/"+lang+"/ledger", artifactID).Error; err != nil {
		t.Fatalf("写激活账本失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO page_stagings (page_id, lang, artifact_id, artifact_hash, draft_version, update_time)
		VALUES (?, ?, ?, 'h', 1, now())
		ON CONFLICT (page_id, lang) DO UPDATE SET artifact_id = EXCLUDED.artifact_id, update_time = now()`,
		pageID, lang, artifactID).Error; err != nil {
		t.Fatalf("写暂存账本失败: %v", err)
	}
}

// enableSecondLocale 给工程补一条非默认语言（默认 zh-CN 由 helper 的工程创建路径给出）。
func enableSecondLocale(t *testing.T, db *gorm.DB, projectID, lang string) {
	t.Helper()
	if err := db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) "+
			"VALUES (?, ?, 1, false, true, now(), now())", projectID, lang).Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}
}

// TestMarkStaleByRegistryVersionIgnoresHistoricalArtifacts 重建后保留旧历史，再重启不误标。
//
// 这是报告 ARCH-03 验收的第一条：页面已经用新组件重建过（账本指向新产物），
// 但旧产物行还在（未 GC）—— 判定不得因为它而把页面重新标成 stale。
func TestMarkStaleByRegistryVersionIgnoresHistoricalArtifacts(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	current := builder.RegistryVersion()
	page := createPage(t, svc, projectID, "/registry-stale-history", emptyRevDoc)

	// 历史产物（旧组件产出、仍 available）+ 当前产物（新组件产出）。
	insertRegistryArtifact(t, db, page.ID, "zh-CN", 1, "old-registry-1")
	curID := insertRegistryArtifact(t, db, page.ID, "zh-CN", 2, current)
	pointPageLedger(t, db, page.ID, "zh-CN", curID)
	if err := db.Exec("UPDATE pages SET stale = false WHERE id = ?", page.ID).Error; err != nil {
		t.Fatalf("清理 stale 失败: %v", err)
	}

	ids, err := svc.MarkStaleByRegistryVersion(ctx, current)
	if err != nil {
		t.Fatalf("启动收敛失败: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("账本指向的是当前版本产物，历史行不该参与判定，实际命中 %v", ids)
	}
	if pageStale(t, db, page.ID) {
		t.Fatal("页面被误标为 stale（旧历史产物仍在 available，但账本已指向新产物）")
	}

	// 用例前提自检：旧产物行确实还在（否则这条用例测不到「历史行不参与」）。
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM page_artifacts WHERE page_id = ? AND registry_version = 'old-registry-1' AND payload_state = 'available'",
		page.ID).Scan(&n).Error; err != nil {
		t.Fatalf("统计历史产物失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("用例前提不成立：旧历史产物行应仍在 available，实际 %d 行", n)
	}
}

// TestMarkStaleByRegistryVersionHitsOtherLangOnly 仅另一语言的产物由旧组件产出也必须命中。
//
// 报告 ARCH-03 验收的第三条：默认语言已经重建（新版本），只有 en-US 还停在旧产物 ——
// 只按默认语言判定会漏掉它，线上 en-US 于是继续跑旧组件字节且无人察觉。
// 同时覆盖「重建收敛」：账本改指新产物后再跑一次，不得再标记。
func TestMarkStaleByRegistryVersionHitsOtherLangOnly(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	current := builder.RegistryVersion()
	enableSecondLocale(t, db, projectID, "en-US")
	page := createPage(t, svc, projectID, "/registry-stale-lang", emptyRevDoc)

	zhCur := insertRegistryArtifact(t, db, page.ID, "zh-CN", 1, current)
	enOld := insertRegistryArtifact(t, db, page.ID, "en-US", 1, "old-registry-2")
	pointPageLedger(t, db, page.ID, "zh-CN", zhCur)
	pointPageLedger(t, db, page.ID, "en-US", enOld)
	if err := db.Exec("UPDATE pages SET stale = false WHERE id = ?", page.ID).Error; err != nil {
		t.Fatalf("清理 stale 失败: %v", err)
	}

	ids, err := svc.MarkStaleByRegistryVersion(ctx, current)
	if err != nil {
		t.Fatalf("启动收敛失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != page.ID {
		t.Fatalf("仅 en-US 由旧组件产出也必须命中页面 %s，实际 %v", page.ID, ids)
	}
	if !pageStale(t, db, page.ID) {
		t.Fatal("页面应被标记为 stale")
	}

	// 重建收敛：en-US 账本改指新组件产物，再跑一次不该再标记。
	if err := db.Exec("UPDATE pages SET stale = false WHERE id = ?", page.ID).Error; err != nil {
		t.Fatalf("清理 stale 失败: %v", err)
	}
	enNew := insertRegistryArtifact(t, db, page.ID, "en-US", 2, current)
	pointPageLedger(t, db, page.ID, "en-US", enNew)
	ids, err = svc.MarkStaleByRegistryVersion(ctx, current)
	if err != nil {
		t.Fatalf("启动收敛失败: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("各语言账本都已指向当前版本产物，不该再标记，实际 %v", ids)
	}
}

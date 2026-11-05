package unit

import (
	"context"
	"testing"

	"go_wp/pkg/builder"

	"gorm.io/gorm"
)

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

func enableSecondLocale(t *testing.T, db *gorm.DB, projectID, lang string) {
	t.Helper()
	if err := db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) "+
			"VALUES (?, ?, 1, false, true, now(), now())", projectID, lang).Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}
}

func TestMarkStaleByRegistryVersionIgnoresHistoricalArtifacts(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	current := builder.RegistryVersion()
	page := createPage(t, svc, projectID, "/registry-stale-history", emptyRevDoc)

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

	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM page_artifacts WHERE page_id = ? AND registry_version = 'old-registry-1' AND payload_state = 'available'",
		page.ID).Scan(&n).Error; err != nil {
		t.Fatalf("统计历史产物失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("用例前提不成立：旧历史产物行应仍在 available，实际 %d 行", n)
	}
}

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

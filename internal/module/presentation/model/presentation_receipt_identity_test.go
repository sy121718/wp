package presentationmodel_test

import (
	"context"
	publish "go_wp/pkg/publish"
	"testing"
	"time"

	"gorm.io/gorm"

	presmodel "go_wp/internal/module/presentation/model"
	"go_wp/public/test/support"
)

func newPresentationModelDB(t *testing.T) (*gorm.DB, *presmodel.Model) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	return db, presmodel.NewModel(db)
}

func seedInstance(t *testing.T, db *gorm.DB, projectID, id string) {
	t.Helper()
	seedProjectOnce(t, db, projectID)
	templateID := seedTemplate(t, db, projectID)
	now := time.Now().UTC()

	entityID := "e0000000-0000-4000-8000-" + id[len(id)-12:]
	if err := db.Exec(`INSERT INTO presentation_instances
		(id, project_id, entity_type, entity_id, template_id, url_path, render_mode,
		 stale, input_version, create_time, update_time)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		id, projectID, "article", entityID, templateID, "/seed/"+id, "template",
		false, 0, now, now).Error; err != nil {
		t.Fatalf("插入实例失败: %v", err)
	}
}

func seedProjectOnce(t *testing.T, db *gorm.DB, projectID string) {
	t.Helper()
	var exists bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM projects WHERE id = ?)", projectID).
		Scan(&exists).Error; err != nil {
		t.Fatalf("查询工程行失败：%v", err)
	}
	if exists {
		return
	}
	support.SeedProjectRow(t, db, projectID, "")
}

func seedTemplate(t *testing.T, db *gorm.DB, projectID string) string {
	t.Helper()
	templateID := "d0000000-0000-4000-8000-" + projectID[len(projectID)-12:]
	var exists bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM content_templates WHERE id = ?)", templateID).
		Scan(&exists).Error; err != nil {
		t.Fatalf("查询内容模板失败：%v", err)
	}
	if exists {
		return templateID
	}
	if err := db.Exec(`INSERT INTO content_templates
		(id, project_id, name, entity_type, draft_document, create_time, update_time)
		VALUES (?, ?, 'presentation 测试模板', 'article', '{}'::jsonb, NOW(), NOW())`,
		templateID, projectID).Error; err != nil {
		t.Fatalf("准备内容模板失败：%v", err)
	}
	return templateID
}

func seedTemplateVersion(t *testing.T, db *gorm.DB, templateID string) string {
	t.Helper()
	versionID := "7e17e17e-0000-4000-8000-" + templateID[len(templateID)-12:]
	var exists bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM content_template_versions WHERE id = ?)", versionID).
		Scan(&exists).Error; err != nil {
		t.Fatalf("查询模板版本失败：%v", err)
	}
	if exists {
		return versionID
	}
	if err := db.Exec(`INSERT INTO content_template_versions
		(id, template_id, version, document, source_hash, created_by, create_time)
		VALUES (?, ?, 1, '{}'::jsonb, ?, ?, NOW())`,
		versionID, templateID, "seed-hash-"+templateID,
		"c0000000-0000-4000-8000-000000000000").Error; err != nil {
		t.Fatalf("准备模板版本失败：%v", err)
	}
	return versionID
}

func TestMarkPublishedLangTxWritesReceiptID(t *testing.T) {
	db, m := newPresentationModelDB(t)
	ctx := context.Background()
	const pid = "11111111-1111-1111-1111-111111111111"
	const iid = "22222222-2222-2222-2222-222222222222"
	seedInstance(t, db, pid, iid)
	now := time.Now().UTC()

	err := m.Transaction(ctx, func(tx *gorm.DB) error {
		if merr := m.MarkPublishedLangTx(tx, presmodel.PublicationRecord{
			PresentationID: iid, Lang: "zh-CN", ActivePath: "/seed/" + iid,
			ArtifactID:   "a0000001-0000-4000-8000-000000000001",
			ArtifactHash: "h1", ReceiptID: "42", PublishedAt: now,
		}); merr != nil {
			return merr
		}
		got, rerr := m.PublishedReceiptIDTx(tx, iid, "zh-CN")
		if rerr != nil {
			return rerr
		}
		if got != "42" {
			t.Fatalf("事务内读回的 receipt_id 应为 42，实际 %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("写入语言账本失败: %v", err)
	}
}

func TestMarkPublishedLangTxWithoutReceiptWritesNull(t *testing.T) {
	db, m := newPresentationModelDB(t)
	ctx := context.Background()
	const pid = "33333333-3333-3333-3333-333333333333"
	const iid = "44444444-4444-4444-4444-444444444444"
	seedInstance(t, db, pid, iid)
	now := time.Now().UTC()

	err := m.Transaction(ctx, func(tx *gorm.DB) error {
		if merr := m.MarkPublishedLangTx(tx, presmodel.PublicationRecord{
			PresentationID: iid, Lang: "zh-CN", ActivePath: "/seed/" + iid,
			ArtifactID:   "a0000002-0000-4000-8000-000000000002",
			ArtifactHash: "h2", PublishedAt: now,
		}); merr != nil {
			return merr
		}
		got, rerr := m.PublishedReceiptIDTx(tx, iid, "zh-CN")
		if rerr != nil {
			return rerr
		}
		if got != "" {
			t.Fatalf("没有回执时应读回空串，实际 %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("写入语言账本失败: %v", err)
	}
	var isNull bool
	if err := db.Raw(`SELECT receipt_id IS NULL FROM presentation_publications
		WHERE presentation_id = ? AND lang = ?`, iid, "zh-CN").Scan(&isNull).Error; err != nil {
		t.Fatalf("读取 receipt_id 空值形态失败: %v", err)
	}
	if !isNull {
		t.Fatalf("无回执必须写 NULL 而不是空字符串")
	}
}

func seedArtifact(t *testing.T, db *gorm.DB, presentationID, artifactID string) {
	t.Helper()
	now := time.Now().UTC()

	artifactID = artifactUUID(artifactID)

	suffix := artifactID[len(artifactID)-12:]
	snapshotID := "5ee5ee5e-0000-4000-8000-" + suffix

	var projectID string
	if err := db.Raw("SELECT project_id::text FROM presentation_instances WHERE id = ?", presentationID).
		Scan(&projectID).Error; err != nil {
		t.Fatalf("读取实例所属工程失败：%v", err)
	}
	templateVersionID := seedTemplateVersion(t, db, seedTemplate(t, db, projectID))
	entityRevisionID := "00000000-0000-4000-8000-" + suffix
	if err := db.Exec(`INSERT INTO document_snapshots
		(id, presentation_instance_id, source_template_version_id, source_entity_revision_id,
		 document, create_time)
		VALUES (?,?,?,?,?,?)`, snapshotID, presentationID, templateVersionID,
		entityRevisionID,
		[]byte(`{}`), now).Error; err != nil {
		t.Fatalf("插入快照行失败: %v", err)
	}

	if err := db.Exec(`INSERT INTO presentation_artifacts
		(id, presentation_instance_id, snapshot_id, version, lang, source_hash,
		 build_input_hash, artifact_provider, artifact_key, artifact_hash, compiler_version,
		 registry_version, manifest, created_by, create_time)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, artifactID, presentationID, snapshotID, 1,
		"zh-CN", "src-"+artifactID, "bih-"+artifactID, "local", "artifacts/"+artifactID,
		"hash-"+artifactID, "c1", "r1", []byte(`{}`), artifactID, now).Error; err != nil {
		t.Fatalf("插入产物行失败: %v", err)
	}
}

func artifactUUID(alias string) string {
	const hex = "0123456789abcdef"
	var out [12]byte
	h := uint64(14695981039346656037)
	for i := 0; i < len(alias); i++ {
		h ^= uint64(alias[i])
		h *= 1099511628211
	}
	for i := 11; i >= 0; i-- {
		out[i] = hex[h&0xf]
		h >>= 4
	}
	return "a0000000-0000-4000-8000-" + string(out[:])
}

func seedDependency(t *testing.T, db *gorm.DB, presentationID, artifactID, kind, key string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO presentation_dependencies
		(presentation_id, artifact_id, dependency_kind, dependency_key, last_checked)
		VALUES (?,?,?,?,?)`, presentationID, artifactID, kind, key, time.Now().UTC()).Error; err != nil {
		t.Fatalf("登记依赖失败: %v", err)
	}
}

func TestMarkInstanceNavigationStaleTxMarksOnlyDeclaringInstances(t *testing.T) {
	db, m := newPresentationModelDB(t)
	ctx := context.Background()
	const pid = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	const hit = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	const miss = "dddddddd-dddd-dddd-dddd-dddddddddddd"
	const navID = "12312312-1231-1231-1231-123123123123"
	seedInstance(t, db, pid, hit)
	seedInstance(t, db, pid, miss)
	key := publish.NavigationKey(navID)
	if key.Kind != publish.DepKindNavigation {
		t.Fatalf("依赖种类必须来自 pipeline 常量，实际 %q", key.Kind)
	}

	seedArtifact(t, db, hit, "art-"+hit)
	seedDependency(t, db, hit, artifactUUID("art-"+hit), key.Kind, key.Key)

	var before int64
	if err := db.Raw("SELECT input_version FROM presentation_instances WHERE id = ?", hit).
		Scan(&before).Error; err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}

	var n int64
	err := m.Transaction(ctx, func(tx *gorm.DB) error {
		var merr error
		n, merr = m.MarkInstanceNavigationStaleTx(ctx, tx, pid, navID, time.Now().UTC())
		return merr
	})
	if err != nil {
		t.Fatalf("按项导航标记失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("只应命中声明了该导航项的实例，实际命中 %d 行", n)
	}
	var after struct {
		Version int64
		Stale   bool
	}
	if err := db.Raw("SELECT input_version AS version, stale FROM presentation_instances WHERE id = ?", hit).
		Scan(&after).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if !after.Stale || after.Version != before+1 {
		t.Fatalf("标记必须 stale=true 且版本 +1：stale=%v before=%d after=%d",
			after.Stale, before, after.Version)
	}
	var missRow struct {
		Stale   bool
		Version int64
	}
	if err := db.Raw("SELECT stale, input_version AS version FROM presentation_instances WHERE id = ?", miss).
		Scan(&missRow).Error; err != nil {
		t.Fatalf("回读未声明实例失败: %v", err)
	}
	if missRow.Stale || missRow.Version != 0 {
		t.Fatalf("未声明该导航项的实例不得被标记：stale=%v version=%d", missRow.Stale, missRow.Version)
	}
}

func TestMarkInstanceNavigationStaleTxRejectsBadInput(t *testing.T) {
	db, m := newPresentationModelDB(t)
	ctx := context.Background()
	const pid = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	const iid = "ffffffff-ffff-ffff-ffff-ffffffffffff"
	seedInstance(t, db, pid, iid)

	err := m.Transaction(ctx, func(tx *gorm.DB) error {
		if _, nerr := m.MarkInstanceNavigationStaleTx(ctx, tx, "", "nav", time.Now().UTC()); nerr == nil {
			t.Fatalf("空工程必须报错")
		}
		if _, nerr := m.MarkInstanceNavigationStaleTx(ctx, tx, pid, "  ", time.Now().UTC()); nerr == nil {
			t.Fatalf("空导航 id 必须报错")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("事务失败: %v", err)
	}
}

func TestPersistPublishInputURLTxAdvancesVersion(t *testing.T) {
	db, m := newPresentationModelDB(t)
	ctx := context.Background()
	const pid = "55555555-5555-5555-5555-555555555555"
	const iid = "66666666-6666-6666-6666-666666666666"
	seedInstance(t, db, pid, iid)

	var got int64
	err := m.Transaction(ctx, func(tx *gorm.DB) error {
		v, perr := m.PersistPublishInputURLTx(tx, pid, iid, "/moved", time.Now().UTC())
		got = v
		return perr
	})
	if err != nil {
		t.Fatalf("持久化输入意图失败: %v", err)
	}
	if got != 1 {
		t.Fatalf("首次推进应从 0 到 1，实际 %d", got)
	}
	var row struct {
		URLPath      string
		InputVersion int64
	}
	if err := db.Raw(`SELECT url_path, input_version FROM presentation_instances WHERE id = ?`, iid).
		Scan(&row).Error; err != nil {
		t.Fatalf("回读实例失败: %v", err)
	}
	if row.URLPath != "/moved" || row.InputVersion != got {
		t.Fatalf("落库事实应与返回值一致：path=%q version=%d（返回 %d）", row.URLPath, row.InputVersion, got)
	}
}

func TestPersistPublishInputURLTxRequiresRange(t *testing.T) {
	db, m := newPresentationModelDB(t)
	const pid = "77777777-7777-7777-7777-777777777777"
	const iid = "88888888-8888-8888-8888-888888888888"
	seedInstance(t, db, pid, iid)

	if _, err := m.PersistPublishInputURLTx(db, pid, iid, "   ", time.Now().UTC()); err == nil {
		t.Fatalf("空路径必须报错")
	}
	if _, err := m.PersistPublishInputURLTx(db, "", iid, "/x", time.Now().UTC()); err == nil {
		t.Fatalf("缺工程作用域必须报错")
	}
}

func TestMarkStaleWithoutVersionKeepsVersion(t *testing.T) {
	db, m := newPresentationModelDB(t)
	ctx := context.Background()
	const pid = "99999999-9999-9999-9999-999999999999"
	const iid = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	seedInstance(t, db, pid, iid)

	if _, err := m.MarkStale(ctx, pid, []string{iid}, time.Now().UTC()); err != nil {
		t.Fatalf("MarkStale 失败: %v", err)
	}
	var v1 int64
	if err := db.Raw("SELECT input_version FROM presentation_instances WHERE id = ?", iid).Scan(&v1).Error; err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}
	if _, err := m.MarkStaleWithoutVersion(ctx, pid, []string{iid}, time.Now().UTC()); err != nil {
		t.Fatalf("MarkStaleWithoutVersion 失败: %v", err)
	}
	var row struct {
		V     int64
		Stale bool
	}
	if err := db.Raw("SELECT input_version AS v, stale FROM presentation_instances WHERE id = ?", iid).
		Scan(&row).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if !row.Stale {
		t.Fatalf("补偿标记必须让实例保持 stale")
	}
	if row.V != v1 {
		t.Fatalf("补偿标记不得推进版本：before=%d after=%d", v1, row.V)
	}
}

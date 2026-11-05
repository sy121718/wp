package pagemodel_test

import (
	"context"
	publish "go_wp/pkg/publish"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pagemodel "go_wp/internal/module/page/model"
	"go_wp/public/test/support"
)

const versionProject = "11111111-2222-3333-4444-555555555555"

type versionFixture struct {
	m  *pagemodel.Model
	db *gorm.DB
}

func newVersionFixture(t *testing.T) *versionFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	support.SeedProjectRow(t, db, versionProject, "代次测试站")
	return &versionFixture{m: pagemodel.NewPageModel(db), db: db}
}

func (f *versionFixture) insertVersionPage(t *testing.T, id, path string) int64 {
	t.Helper()
	if err := f.db.Exec(`
		INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document,
			draft_version, stale, create_time, update_time)
		VALUES (?, ?, 'home', 'none', ?, '{"root":[]}'::jsonb, 1, false, NOW(), NOW())`,
		id, versionProject, path).Error; err != nil {
		t.Fatalf("准备页面行失败：%v", err)
	}
	var v int64
	if err := f.db.Raw("SELECT input_version FROM pages WHERE id = ?", id).Scan(&v).Error; err != nil {
		t.Fatalf("读取初始代次失败：%v", err)
	}
	return v
}

func (f *versionFixture) readInputVersion(t *testing.T, id string) int64 {
	t.Helper()
	var v int64
	if err := f.db.Raw("SELECT input_version FROM pages WHERE id = ?", id).Scan(&v).Error; err != nil {
		t.Fatalf("读取代次失败：%v", err)
	}
	return v
}

func (f *versionFixture) readStale(t *testing.T, id string) bool {
	t.Helper()
	var stale bool
	if err := f.db.Raw("SELECT stale FROM pages WHERE id = ?", id).Scan(&stale).Error; err != nil {
		t.Fatalf("读取 stale 失败：%v", err)
	}
	return stale
}

func (f *versionFixture) insertVersionArtifact(t *testing.T, artifactID, pageID string, version int) {
	t.Helper()
	if err := f.db.Exec(`
		INSERT INTO page_artifacts (id, page_id, version, source_document, page_document_schema_version,
			source_hash, build_input_manifest, build_input_hash, artifact_provider, artifact_key,
			artifact_hash, compiler_version, registry_version, manifest, created_by, create_time)
		VALUES (?, ?, ?, '{}'::jsonb, 1, ?, '{}'::jsonb, ?, 'local', ?, ?, 'c1', 'r1', '{}'::jsonb, ?, NOW())`,
		artifactID, pageID, version, "src-"+artifactID, "bin-"+artifactID,
		"key-"+artifactID, "hash-"+artifactID, uuid.NewString()).Error; err != nil {
		t.Fatalf("准备产物行失败：%v", err)
	}
}

func TestSaveDraftAdvancesInputVersion(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000001"
	before := f.insertVersionPage(t, pageID, "/about")

	revision := &pagemodel.RevisionEntity{
		ID:            uuid.NewString(),
		PageID:        pageID,
		Version:       2,
		DraftPath:     "/about",
		DraftDocument: []byte(`{"root":[{"id":"n1"}]}`),
		SourceHash:    "src-about-2",
		CreatedAt:     time.Now().UTC(),
	}
	ctx := context.Background()
	if err := f.m.SaveDraftWithRevision(ctx, versionProject, pageID, 1, "/about",
		[]byte(`{"root":[{"id":"n1"}]}`), 2, time.Now().UTC(), revision); err != nil {
		t.Fatalf("保存草稿失败：%v", err)
	}

	if got := f.readInputVersion(t, pageID); got != before+1 {
		t.Fatalf("草稿保存应把代次推进 1（%d → %d），实际 %d", before, before+1, got)
	}
	if !f.readStale(t, pageID) {
		t.Fatal("草稿保存后页面应为 stale")
	}
}

func TestMoveDraftPathAdvancesInputVersion(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000002"
	before := f.insertVersionPage(t, pageID, "/about")

	if err := f.m.MoveDraftPath(context.Background(), versionProject, pageID, "/company", time.Now().UTC()); err != nil {
		t.Fatalf("改 URL 失败：%v", err)
	}
	if got := f.readInputVersion(t, pageID); got != before+1 {
		t.Fatalf("改 URL 应推进代次（%d → %d），实际 %d", before, before+1, got)
	}
}

func TestExcludedLangsAdvancesInputVersion(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000003"
	before := f.insertVersionPage(t, pageID, "/about")

	if err := f.m.UpdateExcludedLangs(context.Background(), versionProject, pageID, []string{"en-US"}); err != nil {
		t.Fatalf("写语言排除失败：%v", err)
	}
	if got := f.readInputVersion(t, pageID); got != before+1 {
		t.Fatalf("语言排除应推进代次（%d → %d），实际 %d", before, before+1, got)
	}
}

func TestThemeSnapshotAdvancesInputVersion(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000004"
	before := f.insertVersionPage(t, pageID, "/about")

	theme := []byte(`{"tokens":{"accent":"#000"}}`)
	if err := f.m.UpdateThemeSnapshot(context.Background(), versionProject, pageID, theme); err != nil {
		t.Fatalf("写主题快照失败：%v", err)
	}
	if got := f.readInputVersion(t, pageID); got != before+1 {
		t.Fatalf("主题快照应推进代次（%d → %d），实际 %d", before, before+1, got)
	}

	structure := []byte(`{"header":"block-1"}`)
	if err := f.m.UpdateStructureSnapshot(context.Background(), versionProject, pageID, structure); err != nil {
		t.Fatalf("写结构快照失败：%v", err)
	}
	if got := f.readInputVersion(t, pageID); got != before+2 {
		t.Fatalf("结构快照应再推进一次（%d → %d），实际 %d", before+1, before+2, got)
	}
}

func TestMarkMenuStaleTxAdvancesAndRollsBack(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000005"
	artifactID := "bbbbbbbb-0000-0000-0000-000000000005"
	before := f.insertVersionPage(t, pageID, "/about")
	f.insertVersionArtifact(t, artifactID, pageID, 1)
	if err := f.db.Exec("UPDATE pages SET staged_artifact_id = ? WHERE id = ?", artifactID, pageID).Error; err != nil {
		t.Fatalf("挂暂存产物失败：%v", err)
	}

	depKey := publish.MenuKey(versionProject, "header")
	if err := f.db.Exec(`
		INSERT INTO page_dependencies (page_id, artifact_id, dependency_kind, dependency_key, last_checked)
		VALUES (?, ?, ?, ?, NOW())`, pageID, artifactID, depKey.Kind, depKey.Key).Error; err != nil {
		t.Fatalf("准备依赖行失败：%v", err)
	}

	ctx := context.Background()
	var hit int64
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		n, err := f.m.MarkMenuStaleTx(ctx, tx, versionProject, "header", time.Now().UTC())
		hit = n
		return err
	}); err != nil {
		t.Fatalf("菜单标记失败：%v", err)
	}
	if hit != 1 {
		t.Fatalf("应命中 1 个页面，实际 %d", hit)
	}
	if got := f.readInputVersion(t, pageID); got != before+1 {
		t.Fatalf("菜单标记应推进代次（%d → %d），实际 %d", before, before+1, got)
	}
	if !f.readStale(t, pageID) {
		t.Fatal("菜单标记后页面应为 stale")
	}

	committed := f.readInputVersion(t, pageID)
	committedStale := f.readStale(t, pageID)

	wantErr := gorm.ErrInvalidTransaction
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		if _, err := f.m.MarkMenuStaleTx(ctx, tx, versionProject, "header", time.Now().UTC()); err != nil {
			return err
		}
		return wantErr
	}); err == nil {
		t.Fatal("外层应返回注入的错误")
	}
	if got := f.readInputVersion(t, pageID); got != committed {
		t.Fatalf("回滚后代次必须回到 %d，实际 %d", committed, got)
	}

	if got := f.readStale(t, pageID); got != committedStale {
		t.Fatalf("回滚后 stale 必须回到标记前（%v），实际 %v", committedStale, got)
	}
}

func TestActivationDoesNotAdvanceInputVersion(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000006"
	artifactID := "bbbbbbbb-0000-0000-0000-000000000006"
	before := f.insertVersionPage(t, pageID, "/about")
	f.insertVersionArtifact(t, artifactID, pageID, 1)

	ctx := context.Background()
	now := time.Now().UTC()
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		snap, err := f.m.LockPageForActivationTx(ctx, tx, versionProject, pageID)
		if err != nil {
			return err
		}
		written, err := f.m.WritePageActivationTx(ctx, tx, versionProject, pagemodel.PublicationRecord{
			PageID: pageID, Lang: "zh-CN", ActivePath: "/about",
			ArtifactID: artifactID, ArtifactHash: "hash-x", PublishedAt: now,
		}, snap.InputVersion)
		if err != nil {
			return err
		}
		if !written {
			t.Fatal("锁内写入应命中（代次未变）")
		}
		return nil
	}); err != nil {
		t.Fatalf("激活写入失败：%v", err)
	}
	if got := f.readInputVersion(t, pageID); got != before {
		t.Fatalf("激活不得推进代次，期望 %d，实际 %d", before, got)
	}
}

func TestActivationPredicateRejectsStaleInput(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000007"
	artifactID := "bbbbbbbb-0000-0000-0000-000000000007"
	captured := f.insertVersionPage(t, pageID, "/about")
	f.insertVersionArtifact(t, artifactID, pageID, 1)

	ctx := context.Background()

	if err := f.m.MoveDraftPath(ctx, versionProject, pageID, "/company", time.Now().UTC()); err != nil {
		t.Fatalf("改 URL 失败：%v", err)
	}

	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		written, err := f.m.WritePageActivationTx(ctx, tx, versionProject, pagemodel.PublicationRecord{
			PageID: pageID, Lang: "zh-CN", ActivePath: "/about",
			ArtifactID: artifactID, ArtifactHash: "hash-x", PublishedAt: time.Now().UTC(),
		}, captured)
		if err != nil {
			return err
		}
		if written {
			t.Fatal("旧代次的激活必须被谓词拦住（written 应为 false）")
		}
		return nil
	}); err != nil {
		t.Fatalf("激活事务不应报错：%v", err)
	}
}

func TestForwardCompletionRefusesWhenLedgerReplaced(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000008"
	artOld := "bbbbbbbb-0000-0000-0000-0000000000a8"
	artMine := "bbbbbbbb-0000-0000-0000-0000000000b8"
	artNewer := "bbbbbbbb-0000-0000-0000-0000000000c8"
	v := f.insertVersionPage(t, pageID, "/about")

	f.insertVersionArtifact(t, artOld, pageID, 1)
	f.insertVersionArtifact(t, artNewer, pageID, 2)
	f.insertVersionArtifact(t, artMine, pageID, 3)

	ctx := context.Background()
	now := time.Now().UTC()

	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		written, err := f.m.WritePageActivationTx(ctx, tx, versionProject, pagemodel.PublicationRecord{
			PageID: pageID, Lang: "zh-CN", ActivePath: "/about",
			ArtifactID: artNewer, ArtifactHash: "hash-newer", PublishedAt: now,
		}, v)
		if err != nil {
			return err
		}
		if !written {
			t.Fatal("准备：新成功激活应写入")
		}
		return nil
	}); err != nil {
		t.Fatalf("准备失败：%v", err)
	}

	expect := f.readInputVersion(t, pageID)

	var written bool
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		var err error
		written, err = f.m.WritePageForwardCompletionTx(ctx, tx, versionProject, pagemodel.PublicationRecord{
			PageID: pageID, Lang: "zh-CN", ActivePath: "/about",
			ArtifactID: artMine, ArtifactHash: "hash-mine", PublishedAt: now,
		}, expect, artOld)
		return err
	}); err != nil {
		t.Fatalf("收敛写入不应报错：%v", err)
	}
	if written {
		t.Fatal("账本已被替代时必须放弃前进（否则会覆盖更新的成功产物）")
	}

	var ledger []struct {
		Lang       string
		ArtifactID *string
	}
	if err := f.db.Raw("SELECT lang, artifact_id FROM page_publications WHERE page_id = ?", pageID).
		Scan(&ledger).Error; err != nil {
		t.Fatalf("读取账本失败：%v", err)
	}
	if len(ledger) == 0 {
		t.Fatal("账本应已有新成功激活写下的那一行")
	}
	for _, row := range ledger {
		if row.ArtifactID == nil || *row.ArtifactID != artNewer {
			t.Fatalf("账本必须保持新成功的那份 %s，实际 lang=%s artifact=%v", artNewer, row.Lang, row.ArtifactID)
		}
	}
}

func TestForwardCompletionWritesWhenLedgerUnchanged(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000009"
	artOld := "bbbbbbbb-0000-0000-0000-0000000000a9"
	artMine := "bbbbbbbb-0000-0000-0000-0000000000b9"
	v := f.insertVersionPage(t, pageID, "/about")
	f.insertVersionArtifact(t, artOld, pageID, 1)
	f.insertVersionArtifact(t, artMine, pageID, 2)

	ctx := context.Background()
	now := time.Now().UTC()
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		if _, err := f.m.WritePageActivationTx(ctx, tx, versionProject, pagemodel.PublicationRecord{
			PageID: pageID, Lang: "zh-CN", ActivePath: "/about",
			ArtifactID: artOld, ArtifactHash: "hash-old", PublishedAt: now,
		}, v); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("准备失败：%v", err)
	}

	expect := f.readInputVersion(t, pageID)

	var written bool
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		var err error
		written, err = f.m.WritePageForwardCompletionTx(ctx, tx, versionProject, pagemodel.PublicationRecord{
			PageID: pageID, Lang: "zh-CN", ActivePath: "/about",
			ArtifactID: artMine, ArtifactHash: "hash-mine", PublishedAt: now,
		}, expect, artOld)
		return err
	}); err != nil {
		t.Fatalf("收敛写入失败：%v", err)
	}
	if !written {
		t.Fatal("账本未被替代时应向前完成")
	}
}

func TestLockPageForActivationReadsVersion(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-00000000000a"
	want := f.insertVersionPage(t, pageID, "/about")

	ctx := context.Background()
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		snap, err := f.m.LockPageForActivationTx(ctx, tx, versionProject, pageID)
		if err != nil {
			return err
		}
		if snap.InputVersion != want {
			t.Fatalf("锁读代次 %d，期望 %d", snap.InputVersion, want)
		}
		return nil
	}); err != nil {
		t.Fatalf("锁读失败：%v", err)
	}
}

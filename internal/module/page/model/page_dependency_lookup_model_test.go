package pagemodel_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pagemodel "go_wp/internal/module/page/model"
	"go_wp/internal/pipeline"
	"go_wp/public/test/support"
)

var lookupDepKey = pipeline.ContentTemplateKey("11111111-1111-1111-1111-111111111111")

var otherDepKey = pipeline.ContentTemplateKey("22222222-2222-2222-2222-222222222222")

type lookupFixture struct {
	m        *pagemodel.Model
	db       *gorm.DB
	projectA string
	projectB string

	hitActive string
	hitStaged string
	hitBoth   string
}

func insertPage(t *testing.T, db *gorm.DB, id, projectID, path, title string, stale, deleted bool) {
	t.Helper()
	doc := fmt.Sprintf(`{"settings":{"seo":{"title":%q}},"root":[]}`, title)
	var deletedAt *time.Time
	if deleted {
		at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		deletedAt = &at
	}
	if err := db.Exec(`
		INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document,
			draft_version, stale, deleted_at, create_time, update_time)
		VALUES (?, ?, 'home', 'none', ?, ?::jsonb, 1, ?, ?, NOW(), NOW())`,
		id, projectID, path, doc, stale, deletedAt).Error; err != nil {
		t.Fatalf("准备页面行失败：%v", err)
	}
}

func insertArtifact(t *testing.T, db *gorm.DB, artifactID, pageID string, version int) {
	t.Helper()
	if err := db.Exec(`
		INSERT INTO page_artifacts (id, page_id, version, source_document, page_document_schema_version,
			source_hash, build_input_manifest, build_input_hash, artifact_provider, artifact_key,
			artifact_hash, compiler_version, registry_version, manifest, created_by, create_time)
		VALUES (?, ?, ?, '{}'::jsonb, 1, ?, '{}'::jsonb, ?, 'local', ?, ?, 'c1', 'r1', '{}'::jsonb, ?, NOW())`,
		artifactID, pageID, version, "h-src-"+artifactID[:8], "h-build-"+artifactID[:8],
		"key-"+artifactID, "h-art-"+artifactID[:8], uuid.NewString()).Error; err != nil {
		t.Fatalf("准备产物行失败：%v", err)
	}
}

func setPointers(t *testing.T, db *gorm.DB, pageID string, active, staged any) {
	t.Helper()
	if err := db.Exec("UPDATE pages SET active_artifact_id = ?, staged_artifact_id = ? WHERE id = ?",
		active, staged, pageID).Error; err != nil {
		t.Fatalf("设置产物指针失败：%v", err)
	}
}

func insertDependency(t *testing.T, db *gorm.DB, artifactID, pageID string, dep pipeline.DepKey) {
	t.Helper()
	if err := db.Exec(`
		INSERT INTO page_dependencies (artifact_id, page_id, dependency_kind, dependency_key, last_checked)
		VALUES (?, ?, ?, ?, NOW())`,
		artifactID, pageID, dep.Kind, dep.Key).Error; err != nil {
		t.Fatalf("准备依赖行失败：%v", err)
	}
}

func buildLookupFixture(t *testing.T) *lookupFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	fx := &lookupFixture{m: pagemodel.NewPageModel(db), db: db, projectA: uuid.NewString(), projectB: uuid.NewString()}
	support.SeedProjectRow(t, db, fx.projectA, "反查工程A")
	support.SeedProjectRow(t, db, fx.projectB, "反查工程B")

	newArtifact := func(pageID string, version int) string {
		id := uuid.NewString()
		insertArtifact(t, db, id, pageID, version)
		return id
	}

	fx.hitActive = uuid.NewString()
	insertPage(t, db, fx.hitActive, fx.projectA, "/hit-active", "首页", false, false)
	a1 := newArtifact(fx.hitActive, 1)
	setPointers(t, db, fx.hitActive, a1, nil)
	insertDependency(t, db, a1, fx.hitActive, lookupDepKey)

	fx.hitStaged = uuid.NewString()
	insertPage(t, db, fx.hitStaged, fx.projectA, "/hit-staged", "关于我们", true, false)
	s2 := newArtifact(fx.hitStaged, 1)
	setPointers(t, db, fx.hitStaged, nil, s2)
	insertDependency(t, db, s2, fx.hitStaged, lookupDepKey)

	fx.hitBoth = uuid.NewString()
	insertPage(t, db, fx.hitBoth, fx.projectA, "/hit-both", "", false, false)
	b1, b2 := newArtifact(fx.hitBoth, 1), newArtifact(fx.hitBoth, 2)
	setPointers(t, db, fx.hitBoth, b1, b2)
	insertDependency(t, db, b1, fx.hitBoth, lookupDepKey)
	insertDependency(t, db, b2, fx.hitBoth, lookupDepKey)

	missOld := uuid.NewString()
	insertPage(t, db, missOld, fx.projectA, "/miss-old", "旧产物", false, false)
	c0, c1 := newArtifact(missOld, 1), newArtifact(missOld, 2)
	setPointers(t, db, missOld, c1, nil)
	insertDependency(t, db, c0, missOld, lookupDepKey)

	missDeleted := uuid.NewString()
	insertPage(t, db, missDeleted, fx.projectA, "/miss-deleted", "已删除", false, true)
	d1 := newArtifact(missDeleted, 1)
	setPointers(t, db, missDeleted, d1, nil)
	insertDependency(t, db, d1, missDeleted, lookupDepKey)

	missOtherProject := uuid.NewString()
	insertPage(t, db, missOtherProject, fx.projectB, "/miss-project", "别的工程", false, false)
	e1 := newArtifact(missOtherProject, 1)
	setPointers(t, db, missOtherProject, e1, nil)
	insertDependency(t, db, e1, missOtherProject, lookupDepKey)

	missOtherKey := uuid.NewString()
	insertPage(t, db, missOtherKey, fx.projectA, "/miss-key", "别的键", false, false)
	f1 := newArtifact(missOtherKey, 1)
	setPointers(t, db, missOtherKey, f1, nil)
	insertDependency(t, db, f1, missOtherKey, otherDepKey)

	return fx
}

func TestListPagesByDependencyHitSet(t *testing.T) {
	fx := buildLookupFixture(t)
	if fx == nil {
		return
	}
	rows, err := fx.m.ListPagesByDependency(context.Background(), fx.projectA, lookupDepKey.Kind, lookupDepKey.Key)
	if err != nil {
		t.Fatalf("反查失败：%v", err)
	}
	ids := make([]string, 0, len(rows))
	titles := map[string]string{}
	for i := range rows {
		ids = append(ids, rows[i].ID)
		titles[rows[i].ID] = rows[i].Title
	}
	sort.Strings(ids)
	want := []string{fx.hitActive, fx.hitBoth, fx.hitBoth, fx.hitStaged}
	sort.Strings(want)
	if len(ids) != len(want) {
		t.Fatalf("命中集合应有 %d 行（hitBoth 的活跃与暂存各一行，去重是 service 的职责），实际 %d 行：%v", len(want), len(ids), ids)
	}
	for i := range ids {
		if ids[i] != want[i] {
			t.Fatalf("命中集合不符：want %v, got %v", want, ids)
		}
	}

	if titles[fx.hitActive] != "首页" || titles[fx.hitStaged] != "关于我们" || titles[fx.hitBoth] != "" {
		t.Fatalf("标题取值不符：%v", titles)
	}
}

func TestListPagesByDependencyDoesNotTouchStale(t *testing.T) {
	fx := buildLookupFixture(t)
	if fx == nil {
		return
	}
	before := readStaleSnapshot(t, fx)
	if _, err := fx.m.ListPagesByDependency(context.Background(), fx.projectA, lookupDepKey.Kind, lookupDepKey.Key); err != nil {
		t.Fatalf("反查失败：%v", err)
	}
	after := readStaleSnapshot(t, fx)
	if len(before) != len(after) {
		t.Fatalf("调用前后页面数不一致：%d → %d", len(before), len(after))
	}
	for id, want := range before {
		if after[id] != want {
			t.Fatalf("只读反查改动了 pages.stale：页面 %s 由 %v 变成 %v", id, want, after[id])
		}
	}

	rows, err := fx.m.ListPagesByDependency(context.Background(), fx.projectA, lookupDepKey.Kind, lookupDepKey.Key)
	if err != nil || len(rows) == 0 {
		t.Fatalf("反查应命中页面（否则上面的「未改动」毫无意义）：rows=%d err=%v", len(rows), err)
	}
}

func TestListPagesByDependencyEmptyKeyReturnsNothing(t *testing.T) {
	fx := buildLookupFixture(t)
	if fx == nil {
		return
	}
	for _, c := range []struct{ kind, key string }{{"", lookupDepKey.Key}, {lookupDepKey.Kind, ""}, {"  ", "  "}} {
		rows, err := fx.m.ListPagesByDependency(context.Background(), fx.projectA, c.kind, c.key)
		if err != nil || rows != nil {
			t.Fatalf("空依赖键（kind=%q key=%q）应返回空集合：rows=%v err=%v", c.kind, c.key, rows, err)
		}
	}
}

func TestListPagesByDependencyRequiresProject(t *testing.T) {
	fx := buildLookupFixture(t)
	if fx == nil {
		return
	}
	for _, pid := range []string{"", "  "} {
		if _, err := fx.m.ListPagesByDependency(context.Background(), pid, lookupDepKey.Kind, lookupDepKey.Key); err == nil {
			t.Fatalf("工程 id=%q 应被拒绝", pid)
		}
	}
}

type staleSnapshotRow struct {
	ID    string
	Stale bool
}

func readStaleSnapshot(t *testing.T, fx *lookupFixture) map[string]bool {
	t.Helper()
	var rows []staleSnapshotRow
	if err := fx.db.Raw("SELECT id::text AS id, stale FROM pages").Scan(&rows).Error; err != nil {
		t.Fatalf("读取 stale 现状失败：%v", err)
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Stale
	}
	return out
}

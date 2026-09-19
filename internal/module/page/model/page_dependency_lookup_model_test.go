package pagemodel_test

// page_dependency_lookup_model_test.go — 依赖键**只读反查**的真库行为验证。
//
// 为什么不放在纯逻辑单测里：本方法的价值全在 SQL 上 —— JOIN 到 pages 的工程谓词与软删谓词、
// 「活跃**或**暂存」产物指针、draft_document.settings.seo.title 的取值，任何一条写错都不会报错，
// 只会让影响面清单多几行 / 少几行（删除保护据此放行或误拦）。这类偏差在纯函数测试里看不见。
//
// 表结构来自**生产迁移**（support.NewMigratedPGTestDB 复制的模板库），测试只补真实父行 ——
// 不手抄 CREATE TABLE（手抄会与生产 schema 静默分叉）。
//
// 连接用的是超级用户，RLS 策略在这里不生效，所以「跨工程不看别人的页面」由 SQL 里显式的
// pages.project_id 谓词验证；策略本身（换非超级角色后的 fail closed）属 public/test/rls 的范围。

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

// lookupDepKey 本次反查用的依赖键（模板 id 用固定值；键的拼法取自 pipeline —— 只有那一处实现）。
var lookupDepKey = pipeline.ContentTemplateKey("11111111-1111-1111-1111-111111111111")

// otherDepKey 同 kind、不同 key：用来证明反查按 key 精确命中，而不是「同 kind 全中」。
var otherDepKey = pipeline.ContentTemplateKey("22222222-2222-2222-2222-222222222222")

// lookupFixture 反查用例的夹具：工程 A 里的七类页面 + 工程 B 的一页。
type lookupFixture struct {
	m        *pagemodel.Model
	db       *gorm.DB
	projectA string
	projectB string
	// hitActive / hitStaged / hitBoth 是应命中的三页
	//（hitBoth 的活跃与暂存产物**都**声明了同一依赖 —— 两行命中同一页面）。
	hitActive string
	hitStaged string
	hitBoth   string
}

// insertPage 插入一条真实 pages 行（kind=home 走 pages_content_contract_check 的无内容绑定分支）。
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

// insertArtifact 插入一条真实 page_artifacts 行（只给 NOT NULL 且无默认值的列）。
//
// version 必须逐产物递增：唯一约束 uk_page_artifacts_page_version_lang 是
// (page_id, version, lang)，同一页面同版本的第二个产物插不进去 —— 本用例要造的
// 「同一页面两个产物都声明同一依赖」正是靠不同版本表达。
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

// setPointers 把页面的活跃 / 暂存产物指针指向给定产物（nil = 不指向）。
func setPointers(t *testing.T, db *gorm.DB, pageID string, active, staged any) {
	t.Helper()
	if err := db.Exec("UPDATE pages SET active_artifact_id = ?, staged_artifact_id = ? WHERE id = ?",
		active, staged, pageID).Error; err != nil {
		t.Fatalf("设置产物指针失败：%v", err)
	}
}

// insertDependency 给某产物登记一条依赖（主键是 artifact_id + kind + key）。
func insertDependency(t *testing.T, db *gorm.DB, artifactID, pageID string, dep pipeline.DepKey) {
	t.Helper()
	if err := db.Exec(`
		INSERT INTO page_dependencies (artifact_id, page_id, dependency_kind, dependency_key, last_checked)
		VALUES (?, ?, ?, ?, NOW())`,
		artifactID, pageID, dep.Kind, dep.Key).Error; err != nil {
		t.Fatalf("准备依赖行失败：%v", err)
	}
}

// buildLookupFixture 建库并铺好全部夹具（应命中的三类 + 不该命中的四类）。
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

	// 1) 活跃产物声明该依赖。
	fx.hitActive = uuid.NewString()
	insertPage(t, db, fx.hitActive, fx.projectA, "/hit-active", "首页", false, false)
	a1 := newArtifact(fx.hitActive, 1)
	setPointers(t, db, fx.hitActive, a1, nil)
	insertDependency(t, db, a1, fx.hitActive, lookupDepKey)

	// 2) 只有暂存产物声明该依赖（该页已标 stale）。
	fx.hitStaged = uuid.NewString()
	insertPage(t, db, fx.hitStaged, fx.projectA, "/hit-staged", "关于我们", true, false)
	s2 := newArtifact(fx.hitStaged, 1)
	setPointers(t, db, fx.hitStaged, nil, s2)
	insertDependency(t, db, s2, fx.hitStaged, lookupDepKey)

	// 3) 活跃与暂存**都**声明同一依赖（两行命中同一页面 —— service 侧必须去重）。
	fx.hitBoth = uuid.NewString()
	insertPage(t, db, fx.hitBoth, fx.projectA, "/hit-both", "", false, false)
	b1, b2 := newArtifact(fx.hitBoth, 1), newArtifact(fx.hitBoth, 2)
	setPointers(t, db, fx.hitBoth, b1, b2)
	insertDependency(t, db, b1, fx.hitBoth, lookupDepKey)
	insertDependency(t, db, b2, fx.hitBoth, lookupDepKey)

	// 4) 历史产物声明了该依赖，但它既不是活跃也不是暂存（已被替换掉的旧产物）。
	missOld := uuid.NewString()
	insertPage(t, db, missOld, fx.projectA, "/miss-old", "旧产物", false, false)
	c0, c1 := newArtifact(missOld, 1), newArtifact(missOld, 2)
	setPointers(t, db, missOld, c1, nil)
	insertDependency(t, db, c0, missOld, lookupDepKey)

	// 5) 已软删的页面。
	missDeleted := uuid.NewString()
	insertPage(t, db, missDeleted, fx.projectA, "/miss-deleted", "已删除", false, true)
	d1 := newArtifact(missDeleted, 1)
	setPointers(t, db, missDeleted, d1, nil)
	insertDependency(t, db, d1, missDeleted, lookupDepKey)

	// 6) 另一个工程的页面声明了同一条依赖。
	missOtherProject := uuid.NewString()
	insertPage(t, db, missOtherProject, fx.projectB, "/miss-project", "别的工程", false, false)
	e1 := newArtifact(missOtherProject, 1)
	setPointers(t, db, missOtherProject, e1, nil)
	insertDependency(t, db, e1, missOtherProject, lookupDepKey)

	// 7) 本工程内声明的是**另一条**依赖键。
	missOtherKey := uuid.NewString()
	insertPage(t, db, missOtherKey, fx.projectA, "/miss-key", "别的键", false, false)
	f1 := newArtifact(missOtherKey, 1)
	setPointers(t, db, missOtherKey, f1, nil)
	insertDependency(t, db, f1, missOtherKey, otherDepKey)

	return fx
}

// TestListPagesByDependencyHitSet 命中集合：**活跃或暂存**产物声明该依赖的未删除页面，仅限本工程。
//
// 四类负例（历史产物 / 已软删 / 别的工程 / 别的键）一个都不能出现 —— 其中任何一类漏进来，
// 删模板时都会拦下一个根本不相干的页面（而拦截型错误的代价是运营去解绑一个无关页面）。
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
	// 标题取自 draft_document.settings.seo.title；没填的那一页是空串（读侧不编造名字）。
	if titles[fx.hitActive] != "首页" || titles[fx.hitStaged] != "关于我们" || titles[fx.hitBoth] != "" {
		t.Fatalf("标题取值不符：%v", titles)
	}
}

// TestListPagesByDependencyDoesNotTouchStale 铁律：只读反查**绝不改** pages.stale。
//
// 断言方式是「调用前后逐页比对库里的现状」：任何把这条读路径接到写路径
// （MarkStaleByDependency 按依赖标记 stale）上的实现都会在这里现形 ——
// 那种实现不报错、不返回异常，只会把相关页面悄悄标成待重建。
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
	// 反查本身仍应返回结果（「没改动」不能是因为「什么都没查到」）。
	rows, err := fx.m.ListPagesByDependency(context.Background(), fx.projectA, lookupDepKey.Kind, lookupDepKey.Key)
	if err != nil || len(rows) == 0 {
		t.Fatalf("反查应命中页面（否则上面的「未改动」毫无意义）：rows=%d err=%v", len(rows), err)
	}
}

// TestListPagesByDependencyEmptyKeyReturnsNothing 空依赖键不返回任何页面（也不报错）。
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

// TestListPagesByDependencyRequiresProject 工程作用域必填：空工程 id 直接拒绝，
// 不退化成「不限工程」（那会把别的工程的页面混进这次影响面）。
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

// staleSnapshotRow 逐页读出 stale 现状用的行。
type staleSnapshotRow struct {
	ID    string
	Stale bool
}

// readStaleSnapshot 读出全部页面（含已软删、跨工程）的 id → stale 现状。
//
// 这里刻意不用被测方法自己的读数：本断言要的是库里的客观现状，
// 用被测路径读会把问题掩盖成自证。
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

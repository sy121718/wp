package rlstest

// rls_page_scope_test.go — page 模块**跨工程扇出入口**的工程作用域护栏（DB-009 第三批）。
//
// 本批处理的是签名里没有工程参数的那批入口：整站标记待重建（MarkStaleForI18n）、
// 按主题/块标记（MarkStaleForTheme / MarkStaleForBlock）、引用统计（CountBlockReference）、
// 全站草稿扫描（ListDrafts）、按依赖源标记（MarkStaleByDependency）。
//
// 它们原先在 model 层隐含「不限工程」：不设 app.project_id 的语句在换非超级角色后
// **静默匹配 0 行 / 返回空集**（fail closed 不报错），表现为「改了块但页面不被标记」
// 「工作台显示全站 0 条草稿」。现在的形态是：model 层工程必填（空则显式报错），
// service 层枚举工程表后**逐工程独立作用域**执行再合并。
//
// 每条断言都具备失败能力（不是「调用没报错」）：
//   - 扇出必须同时覆盖两个工程 —— 把任一工程的作用域摘掉立刻红；
//   - 统计类入口的返回值必须等于两工程之和（漏一个工程会少算）；
//   - 跨工程隔离：拿 A 的作用域读不到 B 的行；
//   - 缺工程作用域时显式失败，而不是静默 0 行；
//   - 单工程部署下这些入口行为不变（父批次要求的回归确认）。
//
// 全程跑在非超级角色下（rlsFixture 保证，并用 rls.BypassedRole 自检）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/rls"
)

// seedPage 在指定工程下落一行页面。
//
// 刻意经 rls.InProjectScope 写入：非超级角色下不带 app.project_id 的 INSERT 会被策略
// 的 WITH CHECK 拒绝 —— 种子数据本身就在证明「写入路径必须作用域化」。
func seedPage(t *testing.T, db *gorm.DB, projectID, path, doc string) string {
	t.Helper()
	id := uuid.NewString()
	err := rls.InProjectScope(context.Background(), db, projectID, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pages (id, project_id, kind, content_target_type, draft_path,
			draft_document, draft_version, stale, create_time, update_time)
			VALUES (?, ?, 'home', 'none', ?, ?::jsonb, 1, false, NOW(), NOW())`,
			id, projectID, path, doc).Error
	})
	if err != nil {
		t.Fatalf("写入页面失败（RLS 生效时写入必须带工程作用域）: %v", err)
	}
	return id
}

// seedPageArtifact 落一行 page_artifacts（page_dependencies 的 (artifact_id, page_id) 外键目标）。
// page_artifacts 没有 project_id 列、不在迁移 215 的清单里，因此不受策略约束，可直插。
func seedPageArtifact(t *testing.T, db *gorm.DB, pageID string) string {
	t.Helper()
	id := uuid.NewString()
	err := db.Exec(`INSERT INTO page_artifacts (id, page_id, version, source_document,
		page_document_schema_version, source_hash, build_input_manifest, build_input_hash,
		artifact_provider, artifact_key, artifact_hash, compiler_version, registry_version,
		manifest, created_by, create_time)
		VALUES (?, ?, 1, '{}'::jsonb, 1, 'h', '{}'::jsonb, 'h', 'local', 'k', 'h', 'v', 'v',
			'{}'::jsonb, ?, NOW())`,
		id, pageID, uuid.NewString()).Error
	if err != nil {
		t.Fatalf("写入产物行失败: %v", err)
	}
	return id
}

// staleCount 在**工程作用域内**数该工程被标记 stale 的页面数。
func staleCount(t *testing.T, db *gorm.DB, projectID string) int64 {
	t.Helper()
	var n int64
	err := rls.InProjectScope(context.Background(), db, projectID, func(tx *gorm.DB) error {
		return tx.Table("pages").Where("project_id = ? AND stale", projectID).Count(&n).Error
	})
	if err != nil {
		t.Fatalf("统计 stale 页面失败: %v", err)
	}
	return n
}

// pageFanoutFixture 造「两个工程 + page service（非超级角色）」。
func pageFanoutFixture(t *testing.T) (*gorm.DB, *pageservice.Service, string, string) {
	t.Helper()
	db, _ := rlsFixture(t)
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	svc := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)
	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	return db, svc, pA, pB
}

// TestRLS_PageFanout_MarkStaleForI18nAcrossProjects 整站标记必须覆盖每一个工程。
func TestRLS_PageFanout_MarkStaleForI18nAcrossProjects(t *testing.T) {
	db, svc, pA, pB := pageFanoutFixture(t)
	ctx := context.Background()
	seedPage(t, db, pA, "/i18n-a", `{"settings":{},"root":[]}`)
	seedPage(t, db, pB, "/i18n-b", `{"settings":{},"root":[]}`)

	if err := svc.MarkStaleForI18n(ctx); err != nil {
		t.Fatalf("MarkStaleForI18n 失败: %v", err)
	}
	for _, pid := range []string{pA, pB} {
		if got := staleCount(t, db, pid); got != 1 {
			t.Fatalf("工程 %s 应被标记 1 页，实际 %d —— 逐工程扇出漏了它", pid, got)
		}
	}
}

// TestRLS_PageFanout_MarkStaleForBlockAndCountAcrossProjects 按块标记与引用统计都跨工程。
func TestRLS_PageFanout_MarkStaleForBlockAndCountAcrossProjects(t *testing.T) {
	db, svc, pA, pB := pageFanoutFixture(t)
	ctx := context.Background()
	const blk = "blk-fanout-0001"
	refDoc := `{"settings":{},"root":[{"id":"n1","type":"core.globalref","props":{"blockId":"` + blk + `"}}]}`
	seedPage(t, db, pA, "/blk-a", refDoc)
	seedPage(t, db, pB, "/blk-b1", refDoc)
	seedPage(t, db, pB, "/blk-b2", refDoc)
	seedPage(t, db, pB, "/blk-none", `{"settings":{},"root":[]}`)

	count, err := svc.CountBlockReference(ctx, blk)
	if err != nil {
		t.Fatalf("CountBlockReference 失败: %v", err)
	}
	if count != 3 {
		t.Fatalf("跨工程引用数应为 3（A 1 + B 2），实际 %d —— 扇出漏了某个工程", count)
	}

	if err := svc.MarkStaleForBlock(ctx, blk); err != nil {
		t.Fatalf("MarkStaleForBlock 失败: %v", err)
	}
	if got := staleCount(t, db, pA); got != 1 {
		t.Fatalf("工程 A 应标记 1 页，实际 %d", got)
	}
	if got := staleCount(t, db, pB); got != 2 {
		t.Fatalf("工程 B 应标记 2 页，实际 %d", got)
	}
}

// TestRLS_PageFanout_MarkStaleByDependencyAcrossProjects 依赖扇出跨工程合并去重。
func TestRLS_PageFanout_MarkStaleByDependencyAcrossProjects(t *testing.T) {
	db, svc, pA, pB := pageFanoutFixture(t)
	ctx := context.Background()
	pageA := seedPage(t, db, pA, "/dep-a", `{"settings":{},"root":[]}`)
	pageB := seedPage(t, db, pB, "/dep-b", `{"settings":{},"root":[]}`)

	// 两页各有一份产物，依赖表指向同一条依赖源（article:<uuid> 跨工程语义）。
	const key = "article:0e1d2c3b-0000-4000-8000-0000000000aa"
	for _, pageID := range []string{pageA, pageB} {
		artID := seedPageArtifact(t, db, pageID)
		if err := rls.InProjectScope(ctx, db, projectOf(pageID, pageA, pA, pB), func(tx *gorm.DB) error {
			return tx.Exec("UPDATE pages SET staged_artifact_id = ? WHERE id = ?", artID, pageID).Error
		}); err != nil {
			t.Fatalf("回填暂存产物失败: %v", err)
		}
		if err := db.Exec(`INSERT INTO page_dependencies (page_id, artifact_id, dependency_kind,
			dependency_key, last_checked) VALUES (?, ?, 'direct_content', ?, NOW())`,
			pageID, artID, key).Error; err != nil {
			t.Fatalf("写入依赖行失败: %v", err)
		}
	}

	ids, err := svc.MarkStaleByDependency(ctx, "direct_content", key)
	if err != nil {
		t.Fatalf("MarkStaleByDependency 失败: %v", err)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if len(ids) != 2 || !seen[pageA] || !seen[pageB] {
		t.Fatalf("应返回两个工程的受影响页面（去重后 2 条），实际 %v", ids)
	}
	if got := staleCount(t, db, pA); got != 1 {
		t.Fatalf("工程 A 应标记 1 页，实际 %d", got)
	}
	if got := staleCount(t, db, pB); got != 1 {
		t.Fatalf("工程 B 应标记 1 页，实际 %d", got)
	}
}

// projectOf 小工具：把页面 id 映射回它的工程（测试内固定两页）。
func projectOf(pageID, pageA, pA, pB string) string {
	if pageID == pageA {
		return pA
	}
	return pB
}

// TestRLS_PageFanout_ListDraftsAcrossProjects 全站草稿扫描覆盖每个工程。
func TestRLS_PageFanout_ListDraftsAcrossProjects(t *testing.T) {
	db, svc, pA, pB := pageFanoutFixture(t)
	ctx := context.Background()
	seedPage(t, db, pA, "/draft-a", `{"settings":{},"root":[]}`)
	seedPage(t, db, pB, "/draft-b", `{"settings":{},"root":[]}`)

	drafts, err := svc.ListDrafts(ctx)
	if err != nil {
		t.Fatalf("ListDrafts 失败: %v", err)
	}
	if len(drafts) != 2 {
		t.Fatalf("全站草稿应为 2 条（A 1 + B 1），实际 %d —— 漏作用域时这里会静默变成 0", len(drafts))
	}
	projects := map[string]bool{}
	for _, d := range drafts {
		projects[d.ProjectID] = true
	}
	if !projects[pA] || !projects[pB] {
		t.Fatalf("两个工程的草稿都应出现，实际工程集合 %v", projects)
	}
}

// TestRLS_PageFanout_ModelRejectsMissingScope model 层的跨工程入口在缺工程时显式报错。
//
// 这是「把无作用域从默认行为改成显式决定」的直接断言：宁可报错，也不许退化成
// 「不限工程」（换非超级角色后那是静默 0 行）。
func TestRLS_PageFanout_ModelRejectsMissingScope(t *testing.T) {
	db, _ := rlsFixture(t)
	m := pagemodel.NewPageModel(db)
	ctx := context.Background()

	if err := m.MarkStaleForI18n(ctx, ""); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("MarkStaleForI18n 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if err := m.MarkStaleForTheme(ctx, "  ", "t-any"); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("MarkStaleForTheme 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if err := m.MarkStaleForBlock(ctx, "", "blk"); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("MarkStaleForBlock 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := m.CountBlockReference(ctx, "", "blk"); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("CountBlockReference 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := m.ListDraftDocuments(ctx, ""); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("ListDraftDocuments 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := m.MarkStaleByIDs(ctx, "", []string{uuid.NewString()}, timeNow()); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("MarkStaleByIDs 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := m.MarkStaleByDependency(ctx, "", "direct_content", "k", timeNow()); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("MarkStaleByDependency 缺工程应 ErrProjectRequired，实际 %v", err)
	}
}

// TestRLS_PageFanout_EmptyProjectTableFailsExplicitly 一个工程都没有时显式失败。
//
// 静默返回空结果会把「读不到工程表」伪装成「没有受影响的页面」—— 那正是本批要消灭的
// fail-silent。这里用 TRUNCATE ... CASCADE 把工程表清空（连带外键子表），模拟这种环境。
func TestRLS_PageFanout_EmptyProjectTableFailsExplicitly(t *testing.T) {
	db, role := rlsFixture(t)
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	if err := db.Exec("RESET ROLE").Error; err != nil {
		t.Fatalf("切回属主身份失败: %v", err)
	}
	if err := db.Exec("TRUNCATE projects CASCADE").Error; err != nil {
		t.Fatalf("清空工程表失败: %v", err)
	}
	if err := db.Exec("SET ROLE " + role).Error; err != nil {
		t.Fatalf("切回非超级角色失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	svc := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)

	if err := svc.MarkStaleForI18n(context.Background()); !errors.Is(err, pageservice.ErrProjectRequired) {
		t.Fatalf("无工程时 MarkStaleForI18n 应 ErrProjectRequired，实际 %v", err)
	}

	if _, err := svc.CountBlockReference(context.Background(), "blk"); !errors.Is(err, pageservice.ErrProjectRequired) {
		t.Fatalf("无工程时 CountBlockReference 应 ErrProjectRequired，实际 %v", err)
	}
}

// TestRLS_PageFanout_SingleProjectUnchanged 单工程部署下这些入口行为不变。
//
// 父批次点名的回归项：入口原先靠「工程唯一」拿到作用域，现在走逐工程枚举 ——
// 单工程下必须仍然成功，且结果与改造前一致（只有该工程的数据被处理）。
func TestRLS_PageFanout_SingleProjectUnchanged(t *testing.T) {
	db, _ := rlsFixture(t)
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	svc := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)
	ctx := context.Background()
	pOnly := uuid.NewString()
	seedProject(t, db, pOnly, "唯一工程")
	seedPage(t, db, pOnly, "/only", `{"settings":{},"root":[{"id":"n1","type":"core.globalref","props":{"blockId":"blk-only"}}]}`)

	if err := svc.MarkStaleForI18n(ctx); err != nil {
		t.Fatalf("单工程下 MarkStaleForI18n 应成功，实际 %v", err)
	}
	if got := staleCount(t, db, pOnly); got != 1 {
		t.Fatalf("单工程下应标记 1 页，实际 %d", got)
	}
	if n, err := svc.CountBlockReference(ctx, "blk-only"); err != nil || n != 1 {
		t.Fatalf("单工程下引用数应为 1，实际 %d（err=%v）", n, err)
	}
	if drafts, err := svc.ListDrafts(ctx); err != nil || len(drafts) != 1 {
		t.Fatalf("单工程下草稿应为 1 条，实际 %d（err=%v）", len(drafts), err)
	}
}

// timeNow 取当前时间（把 time 依赖收敛到一处，避免各用例重复 import）。
func timeNow() time.Time { return time.Now().UTC() }

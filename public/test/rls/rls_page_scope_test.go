package rlstest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/rls"
)

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

	if _, err := svc.MarkStaleForBlock(ctx, blk); err != nil {
		t.Fatalf("MarkStaleForBlock 失败: %v", err)
	}
	if got := staleCount(t, db, pA); got != 1 {
		t.Fatalf("工程 A 应标记 1 页，实际 %d", got)
	}
	if got := staleCount(t, db, pB); got != 2 {
		t.Fatalf("工程 B 应标记 2 页，实际 %d", got)
	}
}

func TestRLS_PageFanout_MarkStaleByDependencyAcrossProjects(t *testing.T) {
	db, svc, pA, pB := pageFanoutFixture(t)
	ctx := context.Background()
	pageA := seedPage(t, db, pA, "/dep-a", `{"settings":{},"root":[]}`)
	pageB := seedPage(t, db, pB, "/dep-b", `{"settings":{},"root":[]}`)

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

func projectOf(pageID, pageA, pA, pB string) string {
	if pageID == pageA {
		return pA
	}
	return pB
}

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

func TestRLS_PageFanout_ModelRejectsMissingScope(t *testing.T) {
	db, _ := rlsFixture(t)
	m := pagemodel.NewPageModel(db)
	ctx := context.Background()

	if _, err := m.MarkStaleForI18n(ctx, ""); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("MarkStaleForI18n 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := m.MarkStaleForTheme(ctx, "  ", "t-any"); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("MarkStaleForTheme 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := m.MarkStaleForBlock(ctx, "", "blk"); !errors.Is(err, pagemodel.ErrProjectRequired) {
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

func TestRLS_PageWritePathsRejectMissingScope(t *testing.T) {
	db, _ := rlsFixture(t)
	m := pagemodel.NewPageModel(db)
	ctx := context.Background()
	id := uuid.NewString()
	at := timeNow()

	if _, err := m.ListRevisions(ctx, "", id); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("ListRevisions 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := m.PruneRevisions(ctx, "", id, 5); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("PruneRevisions 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if err := m.MoveDraftPath(ctx, "", id, "/x", at); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("MoveDraftPath 缺工程应 ErrProjectRequired，实际 %v", err)
	}

	if err := m.SaveDraftWithRevision(ctx, "", id, 1, "/x", []byte(`{}`), 2, at,
		&pagemodel.RevisionEntity{ID: uuid.NewString(), PageID: id, Version: 2}); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("SaveDraftWithRevision 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if err := m.ReplaceDependencies(ctx, "", id, uuid.NewString(), nil); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("ReplaceDependencies 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := m.CountDependenciesByKind(ctx, "", id, "direct_content"); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("CountDependenciesByKind 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := m.DeleteStaleRevisions(ctx, "", 20, at, 10); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("DeleteStaleRevisions 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := m.FindPagesByIDs(ctx, "", []string{id}); !errors.Is(err, pagemodel.ErrProjectRequired) {
		t.Fatalf("FindPagesByIDs 缺工程应 ErrProjectRequired，实际 %v", err)
	}
}

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

func TestRLS_PageLocateByIdAcrossProjects(t *testing.T) {
	db, svc, pA, pB := pageFanoutFixture(t)
	ctx := context.Background()
	idB := seedPage(t, db, pB, "/locate-b", `{"settings":{"layout":{"mode":"full"}},"root":[]}`)

	pid, err := svc.ProjectOfPage(ctx, idB)
	if err != nil {
		t.Fatalf("多工程下 ProjectOfPage 应成功: %v", err)
	}
	if pid != pB {
		t.Fatalf("应定位到工程 B，实际 %s", pid)
	}

	if _, err := svc.ListRevisions(ctx, &pagedto.RevisionReq{PageID: idB}); err != nil {
		t.Fatalf("多工程下列出本页修订应成功: %v", err)
	}
	if _, err := svc.ListRevisions(ctx, &pagedto.RevisionReq{PageID: uuid.NewString()}); !errors.Is(err, pageservice.ErrPageNotFound) {
		t.Fatalf("不存在的页面应 ErrPageNotFound，实际: %v", err)
	}

	m := pagemodel.NewPageModel(db)
	if _, err := m.ListRevisions(ctx, pA, idB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("拿 A 的作用域读 B 的页面修订应 ErrRecordNotFound，实际: %v", err)
	}
	if _, err := m.ListRevisions(ctx, pB, idB); err != nil {
		t.Fatalf("拿 B 的作用域读本页修订应成功，实际: %v", err)
	}

	if list, err := m.FindPagesByIDs(ctx, pB, []string{idB}); err != nil || len(list) != 1 {
		t.Fatalf("拿 B 的作用域应取到 1 个页面，实际 %d（err=%v）", len(list), err)
	}
	if list, err := m.FindPagesByIDs(ctx, pA, []string{idB}); err != nil || len(list) != 0 {
		t.Fatalf("拿 A 的作用域不应取到 B 的页面，实际 %d 条（err=%v）", len(list), err)
	}
	if _, err := svc.ProjectOfPage(ctx, uuid.NewString()); !errors.Is(err, pageservice.ErrPageNotFound) {
		t.Fatalf("不存在的页面应 ErrPageNotFound，实际: %v", err)
	}

	if err := svc.Delete(ctx, &pagedto.DeleteReq{ID: idB}); err != nil {
		t.Fatalf("多工程下删除本工程页面应成功: %v", err)
	}
	if _, err := svc.ProjectOfPage(ctx, idB); !errors.Is(err, pageservice.ErrPageNotFound) {
		t.Fatalf("软删后应查不到，实际: %v", err)
	}
	_ = pA
}

func timeNow() time.Time { return time.Now().UTC() }

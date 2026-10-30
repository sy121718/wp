package unit

// page_stale_page_ids_test.go —— 「按依赖键重建」的候选来源端口（ListStalePageIDs）。
//
// 背景：菜单译文 / 按项菜单变更后，失效执行器要重建受影响页面。它原先只认草稿列表，
// 而**已发布**的页面不在草稿列表里 —— 于是「线上正在服务的那一面」永远不重建，
// 用户看到的还是旧菜单。这个端口就是那一批候选的唯一来源。
//
// 判据（三条，缺一不可）：
//   - 只含本工程（工程隔离：别的工程的 stale 页面不得混进来）；
//   - 只含 stale = true 且未软删（软删的页面不重建）；
//   - 已发布与未发布**都要**（是否已发布由调用方自己的发布口径再筛）。
//
// 排序被单独钉住：update_time ASC, id ASC。不排序时超限工程会反复重建同一小撮，
// 剩下的永远轮不到 —— 表现是「每次重建 20 个、线上一直不收敛」。

import (
	"context"
	"testing"

	pagemodel "go_wp/internal/module/page/model"
	projectdto "go_wp/internal/module/project/dto"
	projectservice "go_wp/internal/module/project/service"

	"gorm.io/gorm"
)

// staleIDsOf 走生产实现读候选列表。
func staleIDsOf(t *testing.T, db *gorm.DB, projectID string) []string {
	t.Helper()
	ids, err := pagemodel.NewPageModel(db).ListStalePageIDs(context.Background(), projectID)
	if err != nil {
		t.Fatalf("读取 stale 页面列表失败: %v", err)
	}
	return ids
}

// TestListStalePageIDsCoversPublishedAndUnpublished 已发布与未发布的 stale 页都要在候选里。
func TestListStalePageIDsCoversPublishedAndUnpublished(t *testing.T) {
	db, svc, _, projectID := newPageService(t)

	published := createPage(t, svc, projectID, "/stale/pub", publishContextDoc).ID
	buildAndPublish(t, svc, published)
	unpublished := createPage(t, svc, projectID, "/stale/draft", publishContextDoc).ID

	if err := db.Exec("UPDATE pages SET stale = true WHERE id IN (?, ?)", published, unpublished).Error; err != nil {
		t.Fatalf("标记 stale 失败: %v", err)
	}

	got := staleIDsOf(t, db, projectID)
	set := make(map[string]bool, len(got))
	for _, id := range got {
		set[id] = true
	}
	if !set[published] {
		t.Fatal("已发布的 stale 页必须出现在候选里（草稿列表覆盖不到它，这正是缺陷所在）")
	}
	if !set[unpublished] {
		t.Fatal("未发布的 stale 页也必须在候选里")
	}
	if len(got) != 2 {
		t.Fatalf("应恰好返回 2 个候选，实际 %d（%v）", len(got), got)
	}
}

// TestListStalePageIDsExcludesDeletedAndOtherProjects 软删与别的工程都不得混入。
func TestListStalePageIDsExcludesDeletedAndOtherProjects(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	otherProject := createOtherProject(t, projects)

	mine := createPage(t, svc, projectID, "/stale/mine", publishContextDoc).ID
	deleted := createPage(t, svc, projectID, "/stale/deleted", publishContextDoc).ID
	foreign := createPage(t, svc, otherProject, "/stale/foreign", publishContextDoc).ID
	if err := db.Exec("UPDATE pages SET stale = true WHERE id IN (?, ?, ?)", mine, deleted, foreign).Error; err != nil {
		t.Fatalf("标记 stale 失败: %v", err)
	}
	if err := db.Exec("UPDATE pages SET deleted_at = now() WHERE id = ?", deleted).Error; err != nil {
		t.Fatalf("软删页面失败: %v", err)
	}

	got := staleIDsOf(t, db, projectID)
	if len(got) != 1 || got[0] != mine {
		t.Fatalf("只应返回本工程未软删的那一页 %s，实际 %v", mine, got)
	}
	foreignIDs := staleIDsOf(t, db, otherProject)
	if len(foreignIDs) != 1 || foreignIDs[0] != foreign {
		t.Fatalf("另一工程应只看到自己的页面 %s，实际 %v", foreign, foreignIDs)
	}
}

// TestListStalePageIDsOrderedByMarkTime 排序：先标先返回（超限分批时保证重试单向前进）。
func TestListStalePageIDsOrderedByMarkTime(t *testing.T) {
	db, svc, _, projectID := newPageService(t)

	first := createPage(t, svc, projectID, "/stale/first", publishContextDoc).ID
	second := createPage(t, svc, projectID, "/stale/second", publishContextDoc).ID
	third := createPage(t, svc, projectID, "/stale/third", publishContextDoc).ID

	// 故意让标记时间与 id 顺序相反：只按 id 排序时这里会通不过。
	statement := "UPDATE pages SET stale = true, update_time = now() - (? || ' minutes')::interval WHERE id = ?"
	for i, id := range []string{first, second, third} {
		if err := db.Exec(statement, 30-i*10, id).Error; err != nil {
			t.Fatalf("设置标记时间失败: %v", err)
		}
	}

	got := staleIDsOf(t, db, projectID)
	want := []string{first, second, third}
	if len(got) != len(want) {
		t.Fatalf("应返回 %d 个候选，实际 %d（%v）", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("顺序应为先标先返回 %v，实际 %v", want, got)
		}
	}
}

// TestListStalePageIDsRejectsEmptyProject 空工程是受控错误（不是全库查询）。
func TestListStalePageIDsRejectsEmptyProject(t *testing.T) {
	db, _, _, _ := newPageService(t)
	if _, err := pagemodel.NewPageModel(db).ListStalePageIDs(context.Background(), "  "); err == nil {
		t.Fatal("空工程必须报错：否则会退化成一次跨工程查询")
	}
}

// createOtherProject 建第二个工程（工程隔离判据的对照组）。
func createOtherProject(t *testing.T, projects *projectservice.Service) string {
	t.Helper()
	p, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "另一个工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	return p.ID
}

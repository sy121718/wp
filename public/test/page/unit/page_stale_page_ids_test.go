package unit

import (
	"context"
	"strconv"
	"testing"

	pagemodel "go_wp/internal/module/page/model"
	projectdto "go_wp/internal/module/project/dto"
	projectservice "go_wp/internal/module/project/service"

	"gorm.io/gorm"
)

func staleIDsOf(t *testing.T, db *gorm.DB, projectID string) []string {
	t.Helper()
	ids, err := pagemodel.NewPageModel(db).ListStalePageIDs(context.Background(), projectID)
	if err != nil {
		t.Fatalf("读取 stale 页面列表失败: %v", err)
	}
	return ids
}

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

func TestListStalePageIDsOrderedByMarkTime(t *testing.T) {
	db, svc, _, projectID := newPageService(t)

	first := createPage(t, svc, projectID, "/stale/first", publishContextDoc).ID
	second := createPage(t, svc, projectID, "/stale/second", publishContextDoc).ID
	third := createPage(t, svc, projectID, "/stale/third", publishContextDoc).ID

	statement := "UPDATE pages SET stale = true, update_time = now() - (? || ' minutes')::interval WHERE id = ?"
	for i, id := range []string{first, second, third} {
		if err := db.Exec(statement, strconv.Itoa(30-i*10), id).Error; err != nil {
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

func TestListStalePageIDsRejectsEmptyProject(t *testing.T) {
	db, _, _, _ := newPageService(t)
	if _, err := pagemodel.NewPageModel(db).ListStalePageIDs(context.Background(), "  "); err == nil {
		t.Fatal("空工程必须报错：否则会退化成一次跨工程查询")
	}
}

func createOtherProject(t *testing.T, projects *projectservice.Service) string {
	t.Helper()
	p, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "另一个工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	return p.ID
}

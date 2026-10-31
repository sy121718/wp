package unit

import (
	"context"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
)

func TestListStalePagesCarriesRebuildFailure(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/stale-rebuild-fail", pageDocument)

	model := pagemodel.NewPageModel(db)
	failedAt := time.Now().Add(-3 * time.Minute)
	if err := model.MarkRebuildFailure(ctx, projectID, page.ID, "build", failedAt); err != nil {
		t.Fatalf("记录重建失败失败: %v", err)
	}

	res, err := svc.ListStalePages(ctx, &pagedto.StalePageListReq{ProjectID: projectID, Limit: 20})
	if err != nil {
		t.Fatalf("读取待重建清单失败: %v", err)
	}
	got := findStale(res.Pages, page.ID)
	if got == nil {
		t.Fatalf("新建页面应出现在待重建清单里（清单共 %d 条）", len(res.Pages))
	}
	if got.RebuildFailedStage != "build" {
		t.Errorf("失败阶段应带出 build，实际 %q", got.RebuildFailedStage)
	}
	if got.RebuildFailedAt == nil {
		t.Fatal("失败时刻应带出（界面上靠它判断「这失败是刚发生的还是很久以前」）")
	}
	if diff := got.RebuildFailedAt.Time().Sub(failedAt); diff > time.Second || diff < -time.Second {
		t.Errorf("失败时刻偏差过大：%v", diff)
	}

	if err := model.ClearRebuildFailure(ctx, projectID, page.ID); err != nil {
		t.Fatalf("清除失败痕迹失败: %v", err)
	}
	res, err = svc.ListStalePages(ctx, &pagedto.StalePageListReq{ProjectID: projectID, Limit: 20})
	if err != nil {
		t.Fatalf("再次读取待重建清单失败: %v", err)
	}
	got = findStale(res.Pages, page.ID)
	if got == nil {
		t.Fatal("页面仍应待重建（清失败痕迹不代表重建完成了）")
	}
	if got.RebuildFailedStage != "" || got.RebuildFailedAt != nil {
		t.Errorf("清空后不应再带出失败痕迹：stage=%q at=%v", got.RebuildFailedStage, got.RebuildFailedAt)
	}
}

func TestMarkRebuildFailureRequiresScope(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	page := createPage(t, svc, projectID, "/stale-scope", pageDocument)

	model := pagemodel.NewPageModel(db)
	if err := model.MarkRebuildFailure(ctx, "  ", page.ID, "plan", time.Now()); err == nil {
		t.Fatal("缺 project_id 应报错（否则业务角色下静默 0 行）")
	}
	if err := model.ClearRebuildFailure(ctx, "", page.ID); err == nil {
		t.Fatal("缺 project_id 应报错")
	}
}

func findStale(pages []pagedto.StalePageResp, id string) *pagedto.StalePageResp {
	for i := range pages {
		if pages[i].ID == id {
			return &pages[i]
		}
	}
	return nil
}

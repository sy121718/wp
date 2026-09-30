package unit

// page_rebuild_failure_test.go — 「最近一次自动重建失败」这条可见性链路的长期回归。
//
// 为什么值得单独钉：stale=true 有两种含义 ——「还没轮到重建」与「重建过了但失败了」。
// 前者等着就好，后者要人查日志；而界面在此之前只有那个布尔值，读的人只能猜。
// 迁移 474 把失败阶段与时刻落到 pages 行上，本文件守的是它**从列到契约投影**这一段：
// 列写进去了、清单带得出来、成功后被真正清掉。
//
// 只测到 DTO 为止（不测模板）：模板那一层由 internal/templates 的抽屉用例守着，
// 那里能拿到兜底文案；这里的 render 路径走真实 i18n，断言译文会与语言环境耦合。
//
// 另有一条刻意的取舍写在这里（代码在 service 侧）：队列消费路径（RunPageBuildJob）
// **只在失败时记、不在成功时清** —— 队列任务按语言拆行，一条成功不等于整页恢复。
// 清空只由整页语义的重建（RebuildStale 的逐页循环）负责。

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

	// 重建成功后必须清空：留着旧时刻会让刚恢复的页面继续显示「重建失败」，
	// 那比不显示更糟 —— 读的人会去查一个已经不存在的问题。
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

// TestMarkRebuildFailureRequiresScope 缺工程作用域必须报错而不是静默 0 行。
//
// pages 带 FORCE 策略：漏作用域时这条 UPDATE 在业务角色下一行都匹配不到，而调用方
// 只会在日志里看到「记录失败阶段失败」—— 页面上的失败原因永远缺失。宁可显式报错。
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

// findStale 在清单里按 id 找一条。
func findStale(pages []pagedto.StalePageResp, id string) *pagedto.StalePageResp {
	for i := range pages {
		if pages[i].ID == id {
			return &pages[i]
		}
	}
	return nil
}

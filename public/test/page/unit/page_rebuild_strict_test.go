package unit

// page_rebuild_strict_test.go — 逐项严格错误传播端口的对外口径。
//
// 背景：RebuildStale 对**整批失败**也返回 nil（它对每个页面「记日志后继续」），
// 所以菜单 stale 的同步重建不能拿它的返回值当成功判据 —— 那正是「只入队立刻成功」
// 的老毛病换了个位置。RebuildStaleStrict 是给这条判据用的同步端口。
//
// 本文件钉住三条口径：
//   1. 不存在的页面在严格模式下带页面 ID 返回错误（而不是跳过）；
//   2. 同一批里宽松模式仍然返回 nil（既有消费者契约一字不变）；
//   3. 空列表两条入口都返回 nil（无副作用）。

import (
	"context"
	"strings"
	"testing"

	pagecontract "go_wp/internal/module/page/contract"
)

// strictRebuilder 取严格模式端口的类型断言（与既有用例取恢复入口同一手法：
// 运维/测试能力不进服务契约）。
func strictRebuilder(t *testing.T, svc pagecontract.PageService) interface {
	RebuildStaleStrict(context.Context, []string) error
} {
	t.Helper()
	r, ok := svc.(interface {
		RebuildStaleStrict(context.Context, []string) error
	})
	if !ok {
		t.Fatal("页面服务未提供逐项严格重建端口")
	}
	return r
}

// TestRebuildStaleStrictReportsMissingPage 严格模式：页面不存在也要报错并带上 ID。
func TestRebuildStaleStrictReportsMissingPage(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	ctx := context.Background()
	missing := "99999999-9999-9999-9999-999999999999"

	err := strictRebuilder(t, svc).RebuildStaleStrict(ctx, []string{missing})
	if err == nil {
		t.Fatal("严格模式下不存在的页面必须返回错误（否则调用方会把「什么都没重建」当成成功）")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("错误必须带上页面 ID 便于定位，实际 %v", err)
	}
}

// TestRebuildStaleBestEffortKeepsNilContract 宽松模式：同一批仍然返回 nil（既有消费者契约）。
//
// 这条断言的是**旧契约不变**：rb routers / project 侧的老调用点依赖它「尽力而为、
// 不因单页失败而失败」。改动宽松口径等于改那些调用点的语义。
func TestRebuildStaleBestEffortKeepsNilContract(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	ctx := context.Background()
	missing := "99999999-9999-9999-9999-999999999998"

	if err := svc.RebuildStale(ctx, []string{missing}); err != nil {
		t.Fatalf("宽松模式对不存在的页面应返回 nil（既有契约），实际 %v", err)
	}
}

// TestRebuildStaleEmptyInputIsNoop 空列表两条入口都不报错。
func TestRebuildStaleEmptyInputIsNoop(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	ctx := context.Background()
	if err := svc.RebuildStale(ctx, nil); err != nil {
		t.Fatalf("空列表宽松模式应无操作，实际 %v", err)
	}
	if err := strictRebuilder(t, svc).RebuildStaleStrict(ctx, nil); err != nil {
		t.Fatalf("空列表严格模式应无操作，实际 %v", err)
	}
}

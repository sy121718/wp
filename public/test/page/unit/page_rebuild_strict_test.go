package unit

import (
	"context"
	"strings"
	"testing"

	pagecontract "go_wp/internal/module/page/contract"
)

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

func TestRebuildStaleBestEffortKeepsNilContract(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	ctx := context.Background()
	missing := "99999999-9999-9999-9999-999999999998"

	if err := svc.RebuildStale(ctx, []string{missing}); err != nil {
		t.Fatalf("宽松模式对不存在的页面应返回 nil（既有契约），实际 %v", err)
	}
}

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

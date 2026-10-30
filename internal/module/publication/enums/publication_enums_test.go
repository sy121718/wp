package pubenums

// publication_enums_test.go — 错误声明处的**状态码与文案事实**。
//
// 判据在声明处（pkg/facing.WithStatus + errFallbacks），出口只读不判。
// 所以这条断言属于 enums 自己的包：把它放在 dto 的测试里，等于让请求形状的
// 测试去验错误声明，改了一边会误伤另一边。

import (
	"net/http"
	"testing"

	"go_wp/pkg/facing"
)

// TestErrProjectMissingDeclaresBadRequest 缺工程是**参数错误**（400），不是内部故障。
func TestErrProjectMissingDeclaresBadRequest(t *testing.T) {
	err := ErrProjectMissing()

	status, ok := facing.StatusOf(err)
	if !ok {
		t.Fatalf("错误未携带状态码：%T", err)
	}
	if status != http.StatusBadRequest {
		t.Fatalf("状态码 = %d，期望 400", status)
	}

	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		t.Fatalf("应命中断业务错误判据：%+v", fact)
	}
	if fact.Key != ErrProjectRequired {
		t.Errorf("key = %q，期望 %q", fact.Key, ErrProjectRequired)
	}
	if fact.Tail != "" {
		t.Errorf("这条错误没有明细段，实际 %q", fact.Tail)
	}
	if fact.Fallback != FallbackFor(ErrProjectRequired) || fact.Fallback == "" {
		t.Errorf("兜底 = %q，与声明处兜底表不一致", fact.Fallback)
	}
}

// TestEveryErrConstantHasFallback 每个 Err* 常量都在兜底表里有一句中文（兜底跟声明处走）。
//
// 与 architecture 的同名判据分工不同：那边扫全仓，这里只守本包，改本包常量时
// 不必等整仓门禁先跑起来。
func TestEveryErrConstantHasFallback(t *testing.T) {
	for _, key := range []string{
		ErrRouteOccupied,
		ErrRouteNotFound,
		ErrReceiptPending,
		ErrInvalidParam,
		ErrProjectRequired,
		ErrRouteActiveRename,
		ErrInternal,
	} {
		if FallbackFor(key) == "" {
			t.Errorf("%q 没有在 errFallbacks 里给中文兜底", key)
		}
	}
}

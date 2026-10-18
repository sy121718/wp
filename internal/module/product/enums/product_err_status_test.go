package productenums

// product_err_status_test.go — 状态码登记的守卫（与兜底表同一判据：双向）。
//
// 两个方向都判失败：
//   · 表里登记了一个**不是本包声明的** Err* 常量 → 失败（改名后残留的旧 key 会永远不生效，
//     而出口只是静默回到默认 400，没有任何地方报错）；
//   · 声明了 Err* 却没人给它分档 —— 这一条**故意不判**：参数类错误就该走默认 400，
//     强行要求每条都有状态码会把「400」变成需要维护的第二份清单。
//
// 另有两条语义判据：工厂产出的错误必须能被 StatusOf 读到状态码，且复合错误
// （fmt.Errorf("%w: 明细", ErrX)）仍保住状态码 —— errors.As 穿包装层这件事一旦坏掉，
// 明细与状态码就只剩一个能活。

import (
	"fmt"
	"testing"

	"go_wp/pkg/facing"
)

// TestErrStatusesOnlyCoverDeclaredKeys 登记表里的 key 必须真的存在（否则就是死条目）。
func TestErrStatusesOnlyCoverDeclaredKeys(t *testing.T) {
	declared := errKeyConstants(t) // 取值 → 常量名
	for key := range errStatuses {
		if _, ok := declared[key]; !ok {
			t.Fatalf("errStatuses 登记了本包不存在的 key %q —— 改名后残留的条目永远不会生效", key)
		}
	}
}

// TestErrFactoryCarriesDeclaredStatus 工厂产出的错误带状态码；未登记的 key 不带（走默认 400）。
func TestErrFactoryCarriesDeclaredStatus(t *testing.T) {
	if got, ok := facing.StatusOf(Err(ErrNotFound)); !ok || got != 404 {
		t.Fatalf("Err(ErrNotFound) 的状态码 = (%d, %v)，want (404, true)", got, ok)
	}
	if got, ok := facing.StatusOf(Err(ErrSlugTaken)); !ok || got != 409 {
		t.Fatalf("Err(ErrSlugTaken) 的状态码 = (%d, %v)，want (409, true)", got, ok)
	}
	if got, ok := facing.StatusOf(Err(ErrNameRequired)); ok {
		t.Fatalf("未登记的 key 不该带状态码，实际 %d（默认档由出口给）", got)
	}
}

// TestCompositeErrorKeepsStatus 复合错误的明细不影响状态码。
func TestCompositeErrorKeepsStatus(t *testing.T) {
	err := fmt.Errorf("%w: %s", Err(ErrSlugTaken), "summer-sale")
	if got, ok := facing.StatusOf(err); !ok || got != 409 {
		t.Fatalf("复合错误的状态码 = (%d, %v)，want (409, true)", got, ok)
	}
	if fact, ok := facing.FactOf(err); !ok || fact.Key != ErrSlugTaken {
		t.Fatalf("复合错误应保住业务 key，实际 %+v（ok=%v）", fact, ok)
	}
}

package producthttp

// product_err_fallback_tables_test.go — 出口层兜底表的守卫 + 「未登记业务 key」的稳定兜底（task-158 §7.3）。
//
// 三张表各有归属，守卫也各在各处：
//   · 本模块 Err* 的展示原文 → internal/module/product/enums 的 ProductErrFallbacks
//     （覆盖面守卫在 productenums 包里）；
//   · 别模块 key（inventory / presentation）→ 各自的 enums 兜底表（product 不再替对方留补丁表）；
//   · 跳过原因（VariantSkip* / BundleMember*）→ variantSkipFallbacks / bundleMemberSkipFallbacks，
//     两张表随合成逻辑一起在 service（取值过不了业务判据 pkg/facing.IsBusinessKey，只会从这两张表查）；
//     覆盖面守卫在 service 包的 product_message_test.go / product_bundle_skip_test.go（漏登一条 = 页面上出现裸 key）。

import (
	"fmt"
	"testing"

	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

// TestBusinessErrTextKeepsDeclarationFallback 业务错误的成品串来自**声明处**的兜底。
//
// service 用 productenums.Err(key) 返回，兜底随错误经 pkg/facing 带出；出口只读事实
// （pkg/response.businessText），i18n 里没有该词条时退回这句中文 —— 页面上不会出现裸 key。
func TestBusinessErrTextKeepsDeclarationFallback(t *testing.T) {
	c, _ := newHXContext(t, "", "")
	key := productenums.ErrNameRequired
	want := productenums.ProductErrFallbacks[key]
	if want == "" {
		t.Fatalf("%q 在 enums 兜底表里没有登记", key)
	}
	fact, ok := facing.FactOf(productenums.Err(key))
	if !ok || !fact.Business {
		t.Fatalf("应判为业务错误，fact=%+v ok=%v", fact, ok)
	}
	if fact.Fallback != want {
		t.Errorf("事实兜底 = %q，want %q（由 productenums.Err 随错误带出）", fact.Fallback, want)
	}
	if got := response.MessageOf(c, "product", productenums.Err(key)); got != want {
		t.Errorf("成品串 = %q，want %q", got, want)
	}
}

// TestBusinessErrDetailStaysInTail 复合错误的明细段不参与取词判定（key 仍可识别）。
//
// service 的复合错误一律 fmt.Errorf("%w: 明细")，判据只看冒号前那一段 —— 明细里带冒号、
// 带中文、带 SQL 都不影响「这是哪条业务错误」。
func TestBusinessErrDetailStaysInTail(t *testing.T) {
	err := fmt.Errorf("%w: 第 3 行", productenums.Err(productenums.ErrNameRequired))
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		t.Fatalf("应判为业务错误，fact=%+v", fact)
	}
	if fact.Key != productenums.ErrNameRequired {
		t.Fatalf("key = %q，want %q", fact.Key, productenums.ErrNameRequired)
	}
	if fact.Tail != "第 3 行" {
		t.Fatalf("明细 = %q，want %q", fact.Tail, "第 3 行")
	}
}

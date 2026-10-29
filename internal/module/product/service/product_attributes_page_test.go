package productservice

// 属性库整页读模型（service.ProductAttributesPage）的档位与归一。
//
// 本文件的桩**只给工程清单**（`newProductPageReadModel` 的 model 是 nil）：于是
// 「一条派生取数都没发」成了可断言的事实 —— 真发了 `CountAttributes` 会打到 nil model 上
// 直接 panic，而不是静默返回零值。计数之后的取页要真实库，由
// public/test/product/feature 的属性页用例覆盖（product_attribute_page_test.go）。

import (
	"context"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

func TestAttributeQueryVariationNormalization(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"1", "1"}, {"0", "0"}, {"yes", ""}, {"  ", ""}} {
		q := normalizedTaxonomyQuery(&productdto.CatalogTaxonomyReq{Variation: tc.in, Page: 999})
		if q.Variation != tc.want {
			t.Fatalf("归一结果=%q，want %q", q.Variation, tc.want)
		}
		result, err := (&Service{}).QueryAttributes(context.Background(), &q)
		if err != nil || len(result.Items) != 0 || result.Page != 1 {
			t.Fatalf("空工程不得查库: %+v %v", result, err)
		}
	}
}

// TestClampPageToTotalOrder 页码收敛与 pkg/pagination.Facts 同规则：越界夹到末页、0 页夹到第 1 页。
//
// 收敛发生在**取数之前**：不收敛时越界页码会取回一个空页，而分页条按收敛后的页码渲染 ——
// 「表格为空、分页条却显示第 2 页」正是这条判据要挡的组合。
func TestClampPageToTotalOrder(t *testing.T) {
	for _, tc := range []struct {
		page  int
		total int64
		want  int
	}{
		{page: 0, total: 100, want: 1},
		{page: -3, total: 100, want: 1},
		{page: 2, total: 100, want: 2},
		{page: 5, total: 100, want: 5},
		{page: 6, total: 100, want: 5},
		{page: 999, total: 100, want: 5},
		{page: 3, total: 0, want: 1},
		{page: 1, total: 21, want: 1},
	} {
		if got := ClampPageToTotal(tc.page, ProductSubListPageSize, tc.total); got != tc.want {
			t.Errorf("ClampPageToTotal(%d, %d, %d) = %d，want %d",
				tc.page, ProductSubListPageSize, tc.total, got, tc.want)
		}
	}
}

package productservice

// 属性库整页读模型（service.ProductAttributesPage）的档位与归一。
//
// 本文件的桩**只给工程清单**（`newProductPageReadModel` 的 model 是 nil）：于是
// 「一条派生取数都没发」成了可断言的事实 —— 真发了 `CountAttributes` 会打到 nil model 上
// 直接 panic，而不是静默返回零值。计数之后的取页要真实库，由
// public/test/product/feature 的属性页用例覆盖（product_attribute_page_test.go）。
//
// 页码收敛（越界夹末页、0/负页归 1、total=0 归 1、size 异常走默认）不在本文件重复测：
// 规则只有一份实现 pkg/pagination.ClampPage，用例表在
// pkg/pagination/pagination_query_test.go 的 TestClampPage。本包此前的 ClampPageToTotal
// 包级别名与同一张用例表的重抄已删 —— 转调层不会让规则更正确，只让「改哪一处才算改」
// 变成两道题。

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

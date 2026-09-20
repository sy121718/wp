package productservice

// product_ref_guard_test.go — 跨工程引用明细文案的字节预算（审计 DB-03 §5.1 第 2 条，PROD-02）。
//
// 为什么值得单独钉住：错误经 ?err= 回带到后台页，读侧 shell.FacingNotice 对超过
// shell.NoticeMaxBytes 的回执**一律判为伪造**（回落归口文案「系统内部错误」）——
// 明细写得越长，反而越可能整条消失，「可定位信息」变成看不见。所以明细必须有硬上限，
// 并且超预算时按「减商品示例 → 减工程清单」逐级收敛，而不是无脑拼长串。
//
// 这里用合成数据覆盖三种形态（单命中 / 多工程多命中且 SKU 很长 / 大量命中），
// 强约束是「任何形态都 ≤ refDetailBudget 且含引用面与总数」。

import (
	"strconv"
	"strings"
	"testing"

	productmodel "go_wp/internal/module/product/model"
)

// longRefProduct 造一个 SKU 特别长的引用方商品（预算最坏情况）。
func longRefProduct(projectID string, i int) productmodel.RefProduct {
	return productmodel.RefProduct{
		ProductID: "11111111-1111-4111-8111-11111111111" + string(rune('0'+i%10)),
		ProjectID: projectID,
		SKUCode:   strings.Repeat("X", 40),
	}
}

func TestRefGuardDetailStaysWithinBudget(t *testing.T) {
	cases := []struct {
		name string
		ref  *productmodel.CrossProjectRef
	}{
		{
			name: "单命中单工程",
			ref: &productmodel.CrossProjectRef{
				RefColumns:    []string{"products.category_ids", "products.primary_category_id"},
				ProjectIDs:    []string{"22222222-2222-4222-8222-222222222222"},
				ProductCounts: map[string]int{"22222222-2222-4222-8222-222222222222": 1},
				Products:      []productmodel.RefProduct{{ProductID: "33333333-3333-4333-8333-333333333333", ProjectID: "22222222-2222-4222-8222-222222222222", SKUCode: "SKU-1"}},
				Total:         1,
			},
		},
		{
			name: "三工程三命中且 SKU 很长",
			ref: func() *productmodel.CrossProjectRef {
				r := &productmodel.CrossProjectRef{
					RefColumns:    []string{"products.bundle_items.options[].variantId"},
					ProductCounts: map[string]int{},
				}
				for i := 0; i < 3; i++ {
					pid := "2222222" + string(rune('0'+i)) + "-2222-4222-8222-222222222222"
					r.ProjectIDs = append(r.ProjectIDs, pid)
					r.ProductCounts[pid] = 1
					r.Products = append(r.Products, longRefProduct(pid, i))
				}
				r.Total = 3
				return r
			}(),
		},
		{
			name: "大量命中多工程",
			ref: func() *productmodel.CrossProjectRef {
				r := &productmodel.CrossProjectRef{
					RefColumns:    []string{"products.tag_ids"},
					ProductCounts: map[string]int{},
				}
				for i := 0; i < 10; i++ {
					pid := "4444444" + string(rune('0'+i%10)) + "-4444-4444-8444-444444444444"
					r.ProjectIDs = append(r.ProjectIDs, pid)
					r.ProductCounts[pid] = 987654
					r.Products = append(r.Products, longRefProduct(pid, i))
				}
				r.Total = len(r.ProjectIDs) * 987654
				return r
			}(),
		},
	}
	for _, tc := range cases {
		detail := refGuardDetail(tc.ref)
		if detail == "" {
			t.Fatalf("%s：有引用时明细不能为空", tc.name)
		}
		if len(detail) > refDetailBudget {
			t.Errorf("%s：明细 %d 字节超过预算 %d：%s", tc.name, len(detail), refDetailBudget, detail)
		}
		if !strings.Contains(detail, tc.ref.RefColumns[0]) {
			t.Errorf("%s：明细必须含引用面 %s：%s", tc.name, tc.ref.RefColumns[0], detail)
		}
		if !strings.Contains(detail, strconv.Itoa(tc.ref.Total)) {
			t.Errorf("%s：明细必须含命中总数 %d：%s", tc.name, tc.ref.Total, detail)
		}
	}
	// 单命中的形态还要能定位到具体商品（SKU 与 id 都在）。
	one := cases[0].ref
	detail := refGuardDetail(one)
	for _, want := range []string{one.Products[0].ProductID, one.Products[0].SKUCode, one.ProjectIDs[0]} {
		if !strings.Contains(detail, want) {
			t.Errorf("单命中形态的明细缺少 %q：%s", want, detail)
		}
	}
}

// 无引用时明细为空（调用方据此决定要不要拒绝）。
func TestRefGuardDetailEmptyWhenNoReference(t *testing.T) {
	if got := refGuardDetail(nil); got != "" {
		t.Errorf("nil 结论的明细应为空，实际 %q", got)
	}
	if got := refGuardDetail(&productmodel.CrossProjectRef{Total: 0}); got != "" {
		t.Errorf("零命中的明细应为空，实际 %q", got)
	}
}

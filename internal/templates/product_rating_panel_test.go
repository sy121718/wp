package templates

import (
	"strings"
	"testing"
)

func TestProductRatingPanelUsesIndependentPermissions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		perms       map[string]bool
		add, delete bool
	}{
		{"无评分权限", map[string]bool{}, false, false},
		{"显式拒绝", map[string]bool{"product:rating_add": false, "product:rating_delete": false}, false, false},
		{"仅新增", map[string]bool{"product:rating_add": true}, true, false},
		{"仅删除", map[string]bool{"product:rating_delete": true}, false, true},
		{"新增和删除", map[string]bool{"product:rating_add": true, "product:rating_delete": true}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := groupDData(map[string]any{
				"Rating":  groupDProductRow().Rating,
				"PermSet": map[string]bool{},
				"Buttons": map[string]bool{
					"product.update":        true,
					"product.rating_add":    tc.add,
					"product.rating_delete": tc.delete,
				},
			})
			out, err := render(t, groupDSet(t), "admin/product/product_rating_panel.html", data, groupDRenderScope())
			if err != nil {
				t.Fatal(err)
			}
			for endpoint, allowed := range map[string]bool{"add": tc.add, "delete": tc.delete} {
				if strings.Contains(out, `hx-post="/api/product/rating/`+endpoint+`"`) != allowed {
					t.Fatalf("评分 %s 入口与独立权限不一致：%s", endpoint, out)
				}
			}
			for _, want := range []string{`id="product-ratings-p1"`, `data-product-rating="4.5"`, "</section>"} {
				if !strings.Contains(out, want) {
					t.Fatalf("评分区域缺少 %q", want)
				}
			}
		})
	}
}

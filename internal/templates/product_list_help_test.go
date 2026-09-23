package templates

import (
	"html"
	"strings"
	"testing"
)

func TestProductListHelpRoutesEditsToEditPage(t *testing.T) {
	data := groupDData(map[string]any{
		"SelectedProject": "pr1", "Err": "",
		"Projects": groupDProjects(), "Products": []map[string]any{groupDProductRow()},
		"WarehouseOptions": []map[string]any{{"ID": "w1", "Label": "苏州仓"}},
	})
	for _, tc := range []struct {
		name, lang, lead, edit, tail, expected string
	}{
		{
			name: "Chinese", lang: "zh-CN", lead: "变体与评分属于单个商品，在商品的", edit: "编辑",
			tail:     "页维护（点行的「编辑」进入）。评分是独立明细：平均值与条数由明细算出，改评分不改动商品字段；「没有评分」与「评分 0 分」是两回事。",
			expected: "变体与评分属于单个商品，在商品的编辑页维护（点行的「编辑」进入）",
		},
		{
			name: "English", lang: "en-US", lead: "Variants and ratings belong to a single product and are maintained on that product's", edit: "Edit",
			tail:     " page (open it via Edit in the row). Ratings live in their own detail table: the average and count are derived from it, and editing a rating never touches the product's own fields. No ratings and rated 0 are different things.",
			expected: "Variants and ratings belong to a single product and are maintained on that product's\u00a0Edit page (open it via Edit in the row)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data["lang"] = tc.lang
			data["t"] = func(key, fallback string) string {
				switch key {
				case "admin.products.hint.detailLead":
					return tc.lead
				case "admin.products.row.edit":
					return tc.edit
				case "admin.products.hint.detailTail":
					return tc.tail
				default:
					return fallback
				}
			}
			out, err := render(t, groupDSet(t), "admin/products", data)
			if err != nil {
				t.Fatal(err)
			}
			plain := html.UnescapeString(strings.NewReplacer("<strong>", "", "</strong>", "").Replace(out))
			if !strings.Contains(plain, tc.expected) {
				t.Errorf("商品列表说明未正确指向编辑页，缺少 %q", tc.expected)
			}
		})
	}
}

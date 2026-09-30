package templates

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func productP2Render(t *testing.T, name string, data map[string]any) *html.Node {
	t.Helper()
	out := assertGroupDPage(t, name, data, "</html>")
	root, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func productP2Table(t *testing.T, root *html.Node) (*html.Node, []*html.Node, []*html.Node) {
	t.Helper()
	tables := peAll(root, func(n *html.Node) bool { return peEl(n, "table") && peClass(n, "data-table") })
	if len(tables) != 1 {
		t.Fatalf("应有唯一数据表，实际 %d", len(tables))
	}
	headers := peAll(tables[0], func(n *html.Node) bool { return peEl(n, "th") })
	body := peAll(tables[0], func(n *html.Node) bool { return peEl(n, "tbody") })[0]
	cells := peAll(body, func(n *html.Node) bool { return peEl(n, "td") })
	return tables[0], headers, cells
}

func productP2Text(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var text string
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		text += productP2Text(child)
	}
	return strings.TrimSpace(text)
}

func TestProductP2ListStatusAndMetrics(t *testing.T) {
	row := groupDProductRow()
	row["Status"] = "published"
	row["RatingAvg"] = "4.50"
	row["RatingCount"] = 12
	root := productP2Render(t, "products", groupDData(map[string]any{
		"SelectedProject": "pr1", "Projects": groupDProjects(), "Err": "",
		"WarehouseOptions": []map[string]any{}, "Products": []map[string]any{row},
	}))
	_, headers, cells := productP2Table(t, root)
	if len(cells) != len(headers) {
		t.Fatalf("商品表头 %d 列、数据行 %d 列", len(headers), len(cells))
	}
	if productP2Text(cells[2]) != "已上架" || len(peAll(cells[2], func(n *html.Node) bool { return peClass(n, "badge") })) != 1 {
		t.Fatalf("商品状态须显示本地化徽章，实际 %q", productP2Text(cells[2]))
	}
	if productP2Text(cells[len(cells)-3]) != "4.50" || productP2Text(cells[len(cells)-2]) != "12" {
		t.Fatalf("评分平均值与条数须分列，实际 %q / %q", productP2Text(cells[len(cells)-3]), productP2Text(cells[len(cells)-2]))
	}
	row["HasRating"] = false
	row["RatingCount"] = 0
	root = productP2Render(t, "products", groupDData(map[string]any{
		"SelectedProject": "pr1", "Projects": groupDProjects(), "Err": "",
		"WarehouseOptions": []map[string]any{}, "Products": []map[string]any{row},
	}))
	_, _, cells = productP2Table(t, root)
	if productP2Text(cells[len(cells)-3]) != "—" || productP2Text(cells[len(cells)-2]) != "0" {
		t.Fatal("无评分与零分须区分，条数仍显示 0")
	}
	root = productP2Render(t, "products", groupDData(map[string]any{
		"SelectedProject": "pr1", "Projects": groupDProjects(), "Err": "",
		"WarehouseOptions": []map[string]any{}, "Products": []map[string]any{},
	}))
	_, headers, cells = productP2Table(t, root)
	if peAttr(cells[0], "colspan") != "12" || len(headers) != 12 {
		t.Fatalf("空态必须对齐 12 列，表头 %d、colspan %s", len(headers), peAttr(cells[0], "colspan"))
	}
}

func TestProductP2AttributeValueCountIsNumber(t *testing.T) {
	tr := TranslateFunc("zh-CN")
	rows := map[string]any{"GroupID": "a1", "Rows": []map[string]any{}, "Tr": tr}
	root := productP2Render(t, "product_attributes", groupDData(map[string]any{
		"SelectedProject": "pr1", "Projects": groupDProjects(), "Err": "",
		"Attributes": []map[string]any{{
			"ID": "a1", "Name": "颜色", "Key": "color", "ValueCount": 4, "IsVariation": true,
			"EditForm":   map[string]any{"Csrf": "tok", "Project": "pr1", "t": tr, "Action": "/admin/product-attributes/update", "IsCreate": false, "GroupID": "a1", "Name": "颜色", "Key": "color", "Sort": 0, "VariationChecked": true, "RowsCtx": rows, "InDrawer": true},
			"ValuesForm": map[string]any{"Csrf": "tok", "Project": "pr1", "t": tr, "Action": "/admin/product-attributes/set-values", "GroupID": "a1", "RowsCtx": rows, "InDrawer": true},
		}},
		"AttrCreateForm": map[string]any{"Csrf": "tok", "Project": "pr1", "t": tr, "Action": "/admin/product-attributes/create", "IsCreate": true, "GroupID": "new", "Name": "", "Key": "", "Sort": 0, "VariationChecked": true, "RowsCtx": map[string]any{"GroupID": "new", "Rows": []map[string]any{}, "Tr": tr}, "InDrawer": true},
	}))
	_, _, cells := productP2Table(t, root)
	if got := productP2Text(cells[4]); got != "4" {
		t.Fatalf("属性值列须为纯数值，实际 %q", got)
	}
}

func TestProductP2SEOFieldsHaveCounters(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		want int
	}{
		// 商品页与品牌页都是 0：SEO 标题 / 描述已分别合并进「商品名 + 副标题」与
		// 「品牌名 + 品牌描述」（2026-09-30），三处（含分类）都不再有独立输入框。
		{"product_edit", productEditPageData(), 0},
		{"product_brands", groupDData(map[string]any{
			"SelectedProject": "pr1", "Projects": groupDProjects(), "Err": "",
			"Brands": []map[string]any{{"ID": "b1", "Name": "山野", "Slug": "outdoor", "Sort": 1, "SEOTitle": "标题", "SEODescription": "描述", "UpdatedAt": "今天"}},
		}), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := productP2Render(t, tc.name, tc.data)
			fields := peAll(root, func(n *html.Node) bool {
				return (peEl(n, "input") || peEl(n, "textarea")) &&
					(peAttr(n, "name") == "seoTitle" || peAttr(n, "name") == "seoDescription")
			})
			if len(fields) != tc.want {
				t.Fatalf("SEO 字段应有 %d 个，实际 %d", tc.want, len(fields))
			}
			for _, field := range fields {
				id, limit := peAttr(field, "id"), "60"
				if peAttr(field, "name") == "seoDescription" {
					limit = "155"
					if !peEl(field, "textarea") {
						t.Error("SEO 描述须为多行输入")
					}
				}
				if id == "" || peAttr(field, "data-counter") != limit {
					t.Errorf("%s 缺少字数建议 %s", peAttr(field, "name"), limit)
				}
				outs := peAll(root, func(n *html.Node) bool { return peAttr(n, "data-counter-out") == id })
				if len(outs) != 1 || peAttr(outs[0], "data-counter-template") == "" {
					t.Errorf("%s 缺少对应计数输出", id)
				}
			}
		})
	}
}

func TestProductP2DetailTemplateKeepsActionIDsWithoutDisplayingThem(t *testing.T) {
	data := groupDData(map[string]any{
		"Err": "", "Ready": true, "SelectedProject": "pr1", "Projects": groupDProjects(),
		"Product":        map[string]any{"ID": "p1", "Name": "商品一", "URLPath": "/p/p-one"},
		"InstanceExists": true, "InstanceStatus": "published", "BoundTemplateID": "tpl1", "DefaultTemplateID": "tpl1",
		"TemplateCount": 1, "InstanceURL": "/p/p-one",
		"Templates": []map[string]any{{"ID": "tpl1", "Name": "默认详情", "DraftVersion": 3, "IsDefault": true, "IsBound": true, "UpdatedAt": "今天"}},
	})
	root := productP2Render(t, "product_detail_template", data)
	table, headers, cells := productP2Table(t, root)
	if len(headers) != 5 || len(cells) != len(headers) {
		t.Fatalf("模板列表应为 5 列，表头 %d，数据行 %d", len(headers), len(cells))
	}
	if strings.Contains(productP2Text(table), "tpl1") || strings.Contains(productP2Text(root), "默认模板 id") {
		t.Fatal("内部模板 ID 不应出现在可见文字中")
	}
	if len(peAll(root, func(n *html.Node) bool { return peEl(n, "option") && peAttr(n, "value") == "tpl1" })) == 0 ||
		len(peAll(root, func(n *html.Node) bool { return peEl(n, "a") && strings.Contains(peAttr(n, "href"), "template=tpl1") })) == 0 {
		t.Fatal("下拉和可视化编辑链接须保留操作用 ID")
	}
	data["Templates"] = []map[string]any{}
	root = productP2Render(t, "product_detail_template", data)
	_, _, cells = productP2Table(t, root)
	if got := peAttr(cells[0], "colspan"); got != "5" {
		t.Fatalf("空态 colspan 应为 5，实际 %q", got)
	}
}

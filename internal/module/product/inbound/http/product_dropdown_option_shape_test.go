package producthttp

import (
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// TestDropdownOptionsSeparateFactsFromData 钉住「含空项的下拉」这种选项事实的形状契约。
//
// 消费它的模板（product_edit.html / products.html / product_bundle.html / product_bundle_form.html）
// 判据是 `{{if isset(o.LabelKey)}}`：
//
//	{{if isset(o.LabelKey)}}{{tr(o.LabelKey, o.LabelFallback)}}{{else}}{{o.Label}}{{end}}
//
// isset 问的是**键在不在**，不是「值空不空」—— 数据行（分类名 / 品牌名 / 商品名，它们不是文案、
// 没有词条）一旦带上 `LabelKey: ""` 这个键，isset 即为真，于是走取词分支渲染出**空白 option**：
// 页面 200、结构完整、没有任何报错（实测 `public/test/product/feature` 的
// `TestTaxonomyAdminPages` 报「商品编辑页缺少 "山野"」）。
//
// 所以数据行只给 `Label`，**不给空的 LabelKey / LabelFallback 键**；这条判据两个方向都守：
// 数据行带上键 → 失败；空项没给非空 LabelKey → 也失败。
func TestDropdownOptionsSeparateFactsFromData(t *testing.T) {
	data := []gin.H{
		{"ID": "cat-2", "Label": "衬衫"},
		{"ID": "cat-3", "Label": "裤装"},
	}
	opts := productPickOptions(productenums.ProductsOptionNoPrimaryCategory, "（不指定主分类）", "cat-2", data)
	if len(opts) != len(data)+1 {
		t.Fatalf("选项数应为 空项 + 数据项 = %d，实际 %d", len(data)+1, len(opts))
	}

	none := opts[0]
	if key, _ := none["LabelKey"].(string); key == "" {
		t.Fatalf("空项必须给非空 LabelKey（否则空项也落进 else 分支，渲染成空白）")
	}
	if fallback, _ := none["LabelFallback"].(string); fallback == "" {
		t.Fatalf("空项必须给中文兜底：词条缺失时页面要仍可读")
	}
	if _, ok := none["Label"]; ok {
		t.Fatalf("空项是文案不是数据，不该带 Label")
	}

	for i, row := range opts[1:] {
		// 键存在性：isset 的真假就取决于这一步，空串也算存在。
		if _, ok := row["LabelKey"]; ok {
			t.Fatalf("数据项 %d 带上了 LabelKey 键（值 %q）—— 模板 isset 会因此走取词分支渲染成空白；"+
				"数据行只给 Label", i, row["LabelKey"])
		}
		if _, ok := row["LabelFallback"]; ok {
			t.Fatalf("数据项 %d 带上了 LabelFallback 键 —— 同 LabelKey，必须整键省略", i)
		}
		if row["Label"] != data[i]["Label"] {
			t.Fatalf("数据项 %d 的 Label 应为 %v，实际 %v", i, data[i]["Label"], row["Label"])
		}
		if row["Selected"] != (row["ID"] == "cat-2") {
			t.Fatalf("数据项 %d 的选中态不对：%v", i, row["Selected"])
		}
	}

	// 同为「空项 + 数据行」形状的另一处（捆绑来源商品下拉）：同一判据必须一起成立。
	products := []*productdto.ProductResp{{ID: "p-1", Name: "衬衫 A"}, {ID: "p-2", Name: "裤装 B"}}
	bundle := bundleSourceProductOptions(products, "p-2")
	if key, _ := bundle[0]["LabelKey"].(string); key == "" {
		t.Fatalf("捆绑来源商品下拉的空项必须给非空 LabelKey")
	}
	for i, row := range bundle[1:] {
		if _, ok := row["LabelKey"]; ok {
			t.Fatalf("捆绑来源商品数据项 %d 带上了 LabelKey 键 —— 同 productPickOptions，整键省略", i)
		}
		if row["Label"] != products[i].Name {
			t.Fatalf("捆绑来源商品数据项 %d 的 Label 应为 %q，实际 %v", i, products[i].Name, row["Label"])
		}
	}
}

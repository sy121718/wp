package productservice

// product_row_options_test.go — 行装配里「含空项的下拉」选项事实的形状契约。
//
// 原 internal/module/product/inbound/http/product_dropdown_option_shape_test.go，随
// 行装配下沉（productRow 从控制器搬进 service/product_row.go）迁到本包：判据没变，
// 位置跟着实现走 —— 断言的是 service 造出来的行，不是控制器的。

import (
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// TestDropdownOptionsSeparateFactsFromData 钉住「含空项的下拉」这种选项事实的形状契约。
//
// 消费它的模板（product_edit.html / products.html / product_bundle.html / product_bundle_form.html）
// 判据是 IsNone：
//
//	{{if o.IsNone}}{{tr(o.LabelKey, o.LabelFallback)}}{{else}}{{o.Label}}{{end}}
//
// 空项的文案是**事实**（Key + 中文兜底），数据项是**数据**（分类名 / 品牌名 / 商品名，翻不了）。
// 两者用显式布尔量分开，而不是「LabelKey 这个键在不在」—— 后者让缺键与空串同形，数据行误给
// 一个空串就会被当空项走取词分支，渲染成一片空白 option（页面 200、结构完整、没有任何报错）。
//
// 这条判据两个方向都守：空项必须 IsNone 且给非空 LabelKey/兜底；数据项必须不是 IsNone、
// 且不带 LabelKey/LabelFallback。
func TestDropdownOptionsSeparateFactsFromData(t *testing.T) {
	data := []productdto.PickOptionResp{
		{Value: "cat-2", Label: "衬衫"},
		{Value: "cat-3", Label: "裤装"},
	}
	opts := productPickOptions(productenums.ProductsOptionNoPrimaryCategory, "（不指定主分类）", "cat-2", data)
	if len(opts) != len(data)+1 {
		t.Fatalf("选项数应为 空项 + 数据项 = %d，实际 %d", len(data)+1, len(opts))
	}

	none := opts[0]
	if !none.IsNone {
		t.Fatalf("首项必须是空项（IsNone）")
	}
	if none.LabelKey == "" {
		t.Fatalf("空项必须给非空 LabelKey（否则空项也落进 else 分支，渲染成空白）")
	}
	if none.LabelFallback == "" {
		t.Fatalf("空项必须给中文兜底：词条缺失时页面要仍可读")
	}
	if none.Label != "" {
		t.Fatalf("空项是文案不是数据，不该带 Label（实际 %q）", none.Label)
	}

	for i, row := range opts[1:] {
		if row.IsNone {
			t.Fatalf("数据项 %d 被标成了空项", i)
		}
		if row.LabelKey != "" || row.LabelFallback != "" {
			t.Fatalf("数据项 %d 带上了文案事实（key=%q fallback=%q）—— 模板会走取词分支渲染成空白", i, row.LabelKey, row.LabelFallback)
		}
		if row.Label != data[i].Label {
			t.Fatalf("数据项 %d 的 Label 应为 %q，实际 %q", i, data[i].Label, row.Label)
		}
		if row.Selected != (row.Value == "cat-2") {
			t.Fatalf("数据项 %d 的选中态不对：%v", i, row.Selected)
		}
	}

	// 同为「空项 + 数据行」形状的另两处：来源下拉与捆绑来源商品下拉。
	// 捆绑页那两条已随取数装配下沉到 service，那边的判据见
	// service/product_bundle_page_test.go 的 TestBundleSourceProductOptionsShape。
}

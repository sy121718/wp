package productservice

// product_row_options_test.go — 行装配里「含空项的下拉」选项形状的契约。
//
// 原 internal/module/product/inbound/http/product_dropdown_option_shape_test.go，随
// 行装配下沉（productRow 从控制器搬进 service/product_row.go）迁到本包：判据没变，
// 位置跟着实现走 —— 断言的是 service 造出来的行，不是控制器的。

import (
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

// TestDropdownOptionsSeparateFactsFromData 钉住「含空项的下拉」的形状契约。
//
// 消费它的模板（product_edit.html / product_bundle.html / product_bundle_form.html）判据是 IsNone：
//
//	{{if o.IsNone}}{{tr("<该下拉的空项 key>", "<中文>")}}{{else}}{{o.Label}}{{end}}

// 空项的文案由模板按用途取词（主分类 / 品牌 / 捆绑来源各是各的句子），数据项是**数据**
// （分类名 / 品牌名 / 商品名，翻不了）。两者用显式布尔量分开，而不是「LabelKey 这个键在不在」
// —— 后者让缺键与空串同形，数据行误给一个空串就会被当空项取词，渲染成一片空白 option
// （页面 200、结构完整、没有任何报错）。
//
// 两个方向都守：空项必须 IsNone 且不带 Label；数据项必须不是 IsNone、Label 非空。
func TestDropdownOptionsSeparateFactsFromData(t *testing.T) {
	data := []productdto.PickOptionResp{
		{Value: "cat-2", Label: "衬衫"},
		{Value: "cat-3", Label: "裤装"},
	}
	opts := productPickOptions("cat-2", data)
	if len(opts) != len(data)+1 {
		t.Fatalf("选项数应为 空项 + 数据项 = %d，实际 %d", len(data)+1, len(opts))
	}

	none := opts[0]
	if !none.IsNone {
		t.Fatalf("首项必须是空项（IsNone）")
	}
	if none.Label != "" {
		t.Fatalf("空项是文案不是数据，不该带 Label（实际 %q）", none.Label)
	}
	if none.Selected {
		t.Fatalf("选了 cat-2 时空项不该是选中态")
	}

	for i, row := range opts[1:] {
		if row.IsNone {
			t.Fatalf("数据项 %d 被标成了空项", i)
		}
		if row.Label != data[i].Label {
			t.Fatalf("数据项 %d 的 Label 应为 %q，实际 %q", i, data[i].Label, row.Label)
		}
		if row.Selected != (row.Value == "cat-2") {
			t.Fatalf("数据项 %d 的选中态不对：%v", i, row.Selected)
		}
	}

	// 空选中时首项才是选中态（「不指定」是默认档）。
	if !productPickOptions("", data)[0].Selected {
		t.Fatalf("选中值为空时空项应为选中态")
	}

	// 同为「空项 + 数据行」形状的另两处：来源下拉与捆绑来源商品下拉。
	// 捆绑页那两条已随取数装配下沉到 service，那边的判据见
	// service/product_bundle_page_test.go 的 TestBundleSourceProductOptionsShape。
}

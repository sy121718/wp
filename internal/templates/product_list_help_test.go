package templates

import (
	"html"
	"strings"
	"testing"
)

// TestProductListHelpStatesEditPageIsTheOnlyEditingSurface 验证商品列表说明使用完整句，且不再拼接碎片。
func TestProductListHelpStatesEditPageIsTheOnlyEditingSurface(t *testing.T) {
	data := productCatalogData(nil)
	out, err := render(t, groupDSet(t), "admin/products", data, groupDRenderScope())
	if err != nil {
		t.Fatal(err)
	}
	plain := html.UnescapeString(out)
	for _, want := range []string{
		"商品属性页定义属性组，同一个属性组可以被多个商品引用",
		"变体与评分属于单个商品，在商品的编辑页维护",
		"「没有评分」与「评分 0 分」是两回事",
		"多语言与商品基本字段在编辑页里改",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("商品列表说明缺少完整句 %q", want)
		}
	}
	for _, old := range []string{"admin.products.hint.detailLead", "admin.products.hint.detailTail", "admin.products.hint.editLead", "admin.products.hint.attrLead"} {
		if strings.Contains(out, old) {
			t.Fatalf("商品列表说明仍消费碎片词条 %q", old)
		}
	}
}

package templates

import (
	productdto "go_wp/internal/module/product/dto"
	"strings"
	"testing"
)

func TestProductWarehouseFactsRenderWithoutDisplayFields(t *testing.T) {
	data := productCatalogData(map[string]any{
		"WarehouseOptions": []productdto.WarehouseOptionResp{{ID: "w1", Name: "仓<一>", Code: "SZ", IsDefault: true}},
		"WarehouseSKUOptions": []productdto.WarehouseSKUGroupResp{{WarehouseID: "w1", WarehouseName: "仓<一>", WarehouseCode: "SZ", IsDefault: true,
			Items: []productdto.WarehouseSKUItemResp{{SKUCode: "SKU-1", ExternalSKU: "EXT&1", TrackQuantity: true}}}},
	})
	for _, name := range []string{"products", "products_new", "product_edit"} {
		out, err := render(t, groupDSet(t), "admin/product/"+name, data, groupDRenderScope())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// 多仓 SKU 明细只出现在编辑页：新建页不再承载多仓表单，
		// 列表页只展示汇总。断言必须跟着真实结构走。
		wants := []string{"仓&lt;一&gt;（SZ）", "</html>"}
		if name == "product_edit" {
			wants = append(wants, "SKU-1 · EXT&amp;1")
		}
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Errorf("%s 缺少格式化后的字段 %q", name, want)
			}
		}
		if strings.Contains(out, "仓<一>") {
			t.Errorf("%s 仓库名称未转义", name)
		}
	}
}

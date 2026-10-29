package templates

import (
	productdto "go_wp/internal/module/product/dto"
	"strings"
	"testing"
)

// 仓库和 SKU 名称只从原始字段格式化，特殊字符在 HTML 属性与文本中都必须转义。
func TestProductWarehouseFactsRenderWithoutDisplayFields(t *testing.T) {
	data := productCatalogData(map[string]any{
		"WarehouseOptions": []productdto.WarehouseOptionResp{{ID: "w1", Name: "仓<一>", Code: "SZ", IsDefault: true}},
		"WarehouseSKUOptions": []productdto.WarehouseSKUGroupResp{{WarehouseID: "w1", WarehouseName: "仓<一>", WarehouseCode: "SZ", IsDefault: true,
			Items: []productdto.WarehouseSKUItemResp{{SKUCode: "SKU-1", ExternalSKU: "EXT&1", TrackQuantity: true}}}},
	})
	for _, name := range []string{"products", "products_new", "product_edit"} {
		out, err := render(t, groupDSet(t), "admin/product/"+name, data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		wants := []string{"仓&lt;一&gt;（SZ）", "</html>"}
		if name != "products" {
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

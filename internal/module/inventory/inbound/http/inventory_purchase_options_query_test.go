package inventoryhttp

// 采购下拉的查询次数判据：与商品 / 变体数量无关。
//
// 原写法是「List 一页商品 + 每个商品 Get 一次 + 每个商品查一次库存行」，
// 100 个商品就是 201 次往返 —— 页面能出，只是慢得不明显。这条测试钉住「各一次」。

import (
	"context"
	"testing"

	inventorycontract "go_wp/internal/module/inventory/contract"
	productcontract "go_wp/internal/module/product/contract"
)

type countingVariantCatalog struct {
	productcontract.VariantOptionsReader
	calls int
}

func (c *countingVariantCatalog) ListVariantOptions(context.Context, string) ([]productcontract.VariantOption, error) {
	c.calls++
	out := make([]productcontract.VariantOption, 0, 50)
	for i := 0; i < 50; i++ {
		out = append(out, productcontract.VariantOption{
			VariantID: "v", ProductID: "p", SKUCode: "SKU", ProductName: "商品",
		})
	}
	return out, nil
}

type countingStockSKU struct {
	inventorycontract.InventoryService
	calls int
}

func (c *countingStockSKU) BareSKUsByVariant(context.Context, string) (map[string]string, error) {
	c.calls++
	return map[string]string{}, nil
}

func TestPurchaseVariantOptionsQueriesOnce(t *testing.T) {
	catalog := &countingVariantCatalog{}
	stock := &countingStockSKU{}
	h := &inventoryPurchasePageHandle{inventory: stock, products: catalog}

	options := h.purchaseVariantOptions(context.Background(), "p1", func(_, fallback string) string { return fallback })

	if len(options) != 50 {
		t.Fatalf("下拉项 = %d，want 50", len(options))
	}
	if catalog.calls != 1 {
		t.Fatalf("变体清单查询 %d 次，want 1（与商品数无关）", catalog.calls)
	}
	if stock.calls != 1 {
		t.Fatalf("裸码映射查询 %d 次，want 1（与商品数无关）", stock.calls)
	}
}

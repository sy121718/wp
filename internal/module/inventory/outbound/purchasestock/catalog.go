// catalog.go — 采购页 SKU 下拉的候选适配器（商品侧变体清单 + 库存侧裸码映射）。
//
// 为什么单独一个类型：采购页的下拉要同时读两个模块的只读口（商品侧变体清单、
// 库存侧「变体 → 仓库裸码」），而采购契约只声明自己需要的窄形状
// （purchasecontract.VariantCatalog）。适配器放在库存侧 =
// **实现方依赖调用方契约**，采购模块因此不认识商品与库存的任何包。
//
// 与 orderstock / purchasestock.Stock 同一手法、同一住处。
package purchasestock

import (
	"context"

	inventorycontract "go_wp/internal/module/inventory/contract"
	productcontract "go_wp/internal/module/product/contract"
	purchasecontract "go_wp/internal/module/purchase/contract"
)

// Catalog 采购页 SKU 候选的只读适配器。
type Catalog struct {
	variants productcontract.VariantOptionsReader
	bareSKUs inventorycontract.StockSKUReader
}

// 编译期断言：适配器必须满足采购侧的候选只读口。
var _ purchasecontract.VariantCatalog = (*Catalog)(nil)

// NewCatalog 构造；两条只读口任一为 nil 时对应能力返回空（页面下拉为空，不 panic）。
func NewCatalog(variants productcontract.VariantOptionsReader,
	bareSKUs inventorycontract.StockSKUReader) *Catalog {
	return &Catalog{variants: variants, bareSKUs: bareSKUs}
}

// ListAllVariantOptions 全工程变体清单（采购行候选）。
func (c *Catalog) ListAllVariantOptions(ctx context.Context, projectID string) ([]purchasecontract.VariantOption, error) {
	if c == nil || c.variants == nil || projectID == "" {
		return nil, nil
	}
	list, err := c.variants.ListAllVariantOptions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]purchasecontract.VariantOption, 0, len(list))
	for _, v := range list {
		out = append(out, purchasecontract.VariantOption{
			VariantID: v.VariantID, ProductID: v.ProductID, ProductName: v.ProductName,
			SKUCode: v.SKUCode, CostPrice: v.CostPrice,
		})
	}
	return out, nil
}

// BareSKUsByVariant 工程内「变体 id → 仓库侧裸码」的映射。
func (c *Catalog) BareSKUsByVariant(ctx context.Context, projectID string) (map[string]string, error) {
	if c == nil || c.bareSKUs == nil || projectID == "" {
		return map[string]string{}, nil
	}
	return c.bareSKUs.BareSKUsByVariant(ctx, projectID)
}

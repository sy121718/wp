package purchasestock

import (
	"context"

	inventorycontract "go_wp/internal/module/inventory/contract"
	productcontract "go_wp/internal/module/product/contract"
	purchasecontract "go_wp/internal/module/purchase/contract"
)

type Catalog struct {
	variants productcontract.VariantOptionsReader
	bareSKUs inventorycontract.StockSKUReader
}

var _ purchasecontract.VariantCatalog = (*Catalog)(nil)

func NewCatalog(variants productcontract.VariantOptionsReader,
	bareSKUs inventorycontract.StockSKUReader) *Catalog {
	return &Catalog{variants: variants, bareSKUs: bareSKUs}
}

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

func (c *Catalog) BareSKUsByVariant(ctx context.Context, projectID string) (map[string]string, error) {
	if c == nil || c.bareSKUs == nil || projectID == "" {
		return map[string]string{}, nil
	}
	return c.bareSKUs.BareSKUsByVariant(ctx, projectID)
}

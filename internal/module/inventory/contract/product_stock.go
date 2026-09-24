package inventorycontract

import (
	"context"

	productcontract "go_wp/internal/module/product/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	"gorm.io/gorm"
)

// ProductStockReader exposes only inventory facts used by product projections and deletion guards.
type ProductStockReader interface {
	VariantStockTotals(ctx context.Context, projectID string, variantIDs []string) (totals map[string]int, err error)
	CountNonZeroStocksByVariant(ctx context.Context, variantID, projectID string) (n int64, err error)
	HasBOMParent(ctx context.Context, variantID, projectID string) (exists bool, err error)
}

// ProductStockLookup holds read-only warehouse and inventory facts for product use cases.
type ProductStockLookup interface {
	ResolveWarehouse(ctx context.Context, projectID, warehouseID string) (ref *productcontract.WarehouseRef, err error)
	GetWarehouseSKU(ctx context.Context, req *inventorydto.GetWarehouseSKUReq) (res *inventorydto.WarehouseSKUResp, err error)
	WarehouseStocksByProducts(ctx context.Context, projectID string, productIDs []string) (out []inventorydto.ProductWarehouseStock, err error)
	ResolveVariantWarehouseCosts(ctx context.Context, projectID string, refs []inventorydto.VariantWarehouseCostRef) (out []inventorydto.VariantWarehouseCost, err error)
	VariantHasStockMovement(ctx context.Context, projectID, variantID string) (exists bool, err error)
}

// ProductStockWriter holds only product-initiated inventory creation and its validation.
// Tx methods use the caller's transaction to preserve product and inventory atomicity.
type ProductStockWriter interface {
	EnsureVariantStock(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (err error)
	EnsureVariantStockTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (err error)
	EnsureVariantStockWithExternalTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode, externalSKU string, quantity *int) (err error)
	NormalizeExternalSKU(raw string) (code string, err error)
	CheckExternalSKUProductScope(ctx context.Context, projectID, warehouseID, externalSKU, productID string) (err error)
}

// ProductStockPort groups the two required capabilities supplied by one inventory service.
type ProductStockPort interface {
	ProductStockLookup
	ProductStockWriter
}

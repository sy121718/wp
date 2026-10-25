package inventorycontract

import (
	"context"

	inventorydto "go_wp/internal/module/inventory/dto"
	productcontract "go_wp/internal/module/product/contract"
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
	// ClaimStockTx 把该仓已有的**未认领**存货行认领到本次新建的变体（迁移 621 + 625）。
	//
	// 「从仓库选」这条来路的核心：仓库里那一批货就是这条 SKU，建商品时要把它挂到新变体上，
	// 否则货仍是 variant_id IS NULL —— 商品侧库存汇总看不见它，页面显示 0 而仓库里有货。
	//
	// 与 EnsureVariantStockTx 同为事务透传：建商品与认领要么全成、要么全不成。
	// 返回错误时调用方整体回滚。
	ClaimStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ClaimStockReq) (err error)
}

// ProductStockPort groups the two required capabilities supplied by one inventory service.
type ProductStockPort interface {
	ProductStockLookup
	ProductStockWriter
}

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
	// EnsureVariantStock / EnsureVariantStockTx / EnsureVariantStockWithExternalTx 都在
	// 「该仓已有同裸码的行」时**复用**那一条，并把它的 id 返回给调用方。
	//
	// 返回 stockID 的理由（迁移 637）：商品侧要把这条货行记进变体的 warehouse_skus，
	// 而它是新建还是复用只有库存侧知道。让调用方建完再按 (仓, 裸码) 反查一次，
	// 等于把「怎么定位一条货行」这件事复制一份到调用方 —— 两处口径迟早不一致。
	// WarehouseStockIDBySKU 按 (仓, 裸码) 取货行 id；不存在时返回空串与 nil 错。
	//
	// 商品侧「从仓库选」必须先问这一句：仓库里的货可以先于商品存在（先收货后建品），
	// 那时要**引用既有行**，而不是按 (变体, 仓) 幂等插入 —— 后者会撞 244 的
	// UNIQUE (warehouse_id, sku_code)（23505 冒到页面上就是内部错误）。
	WarehouseStockIDBySKU(ctx context.Context, projectID, warehouseID, skuCode string) (stockID string, err error)
	EnsureVariantStock(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (stockID string, err error)
	EnsureVariantStockTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (stockID string, err error)
	EnsureVariantStockWithExternalTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode, externalSKU string, quantity *int) (stockID string, err error)
	NormalizeExternalSKU(raw string) (code string, err error)
	CheckExternalSKUProductScope(ctx context.Context, projectID, warehouseID, externalSKU, productID string) (err error)
}

// ProductStockPort groups the two required capabilities supplied by one inventory service.
type ProductStockPort interface {
	ProductStockLookup
	ProductStockWriter
}

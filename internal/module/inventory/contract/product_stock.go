package inventorycontract

import (
	"context"

	inventorydto "go_wp/internal/module/inventory/dto"
	productcontract "go_wp/internal/module/product/contract"
	"gorm.io/gorm"
)

type StockFact struct {
	StockID       string
	WarehouseID   string
	WarehouseCode string
	WarehouseName string
	SKUCode       string

	Quantity         int
	ReservedQuantity int

	TrackQuantity bool

	CostPrice *float64

	Name     string
	ImageURL string
}

type ProductStockReader interface {
	VariantDeletionFactsTx(context.Context, *gorm.DB, string, []string, []string) (*VariantDeletionFacts, error)

	StockFactsByStockIDs(ctx context.Context, projectID string, stockIDs []string) (facts []*StockFact, err error)

	VariantHasStockMovement(ctx context.Context, projectID, variantID string) (exists bool, err error)
}

type ProductStockLookup interface {
	VariantDeletionFactsTx(context.Context, *gorm.DB, string, []string, []string) (*VariantDeletionFacts, error)
	ResolveWarehouse(ctx context.Context, projectID, warehouseID string) (ref *productcontract.WarehouseRef, err error)
	GetWarehouseSKU(ctx context.Context, req *inventorydto.GetWarehouseSKUReq) (res *inventorydto.WarehouseSKUResp, err error)

	StockFactsByStockIDs(ctx context.Context, projectID string, stockIDs []string) (facts []*StockFact, err error)

	WarehouseStocksByProducts(ctx context.Context, projectID string, productIDs []string) (out []inventorydto.ProductWarehouseStock, err error)

	ResolveVariantWarehouseCosts(ctx context.Context, projectID string, refs []inventorydto.VariantWarehouseCostRef) (out []inventorydto.VariantWarehouseCost, err error)
	VariantHasStockMovement(ctx context.Context, projectID, variantID string) (exists bool, err error)
}

type ProductStockWriter interface {
	WarehouseStockIDBySKU(ctx context.Context, projectID, warehouseID, skuCode string) (stockID string, err error)
	EnsureVariantStock(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (stockID string, err error)
	EnsureVariantStockTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (stockID string, err error)
	EnsureVariantStockWithExternalTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode, externalSKU string, quantity *int) (stockID string, err error)
	NormalizeExternalSKU(raw string) (code string, err error)
	CheckExternalSKUProductScope(ctx context.Context, projectID, warehouseID, externalSKU, productID string) (err error)

	CalibrateVariantStocksTx(ctx context.Context, tx *gorm.DB, projectID, productID, variantID string, targets []ProductStockTarget) (err error)
}

type ProductStockTarget struct {
	StockID     string
	WarehouseID string
	SKUCode     string
	Quantity    int
}

type ProductStockPort interface {
	ProductStockLookup
	ProductStockWriter
}

type VariantDeletionFacts struct {
	Stocks             []*StockFact
	MovementVariantIDs []string
}

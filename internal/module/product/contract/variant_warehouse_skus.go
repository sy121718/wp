package productcontract

import (
	"context"

	"gorm.io/gorm"
)

type VariantWarehouseStockPort interface {
	WarehouseStockIDsByVariant(ctx context.Context, projectID string, variantIDs []string) (ids map[string][]string, err error)

	VariantIDsByWarehouseStock(ctx context.Context, projectID, stockID string) (variantIDs []string, err error)

	VariantProductIDs(ctx context.Context, projectID string, variantIDs []string) (byVariant map[string]string, err error)

	VariantSKUCodes(ctx context.Context, projectID string, variantIDs []string) (byVariant map[string]string, err error)

	AttachWarehouseStockTx(ctx context.Context, tx *gorm.DB, projectID, variantID string, ref WarehouseStockRef) error

	VariantIDsByWarehouseStocks(ctx context.Context, projectID string, stockIDs []string) (byStock map[string][]string, err error)
}

type WarehouseStockRef struct {
	StockID       string
	WarehouseID   string
	WarehouseCode string
	SKUCode       string
}

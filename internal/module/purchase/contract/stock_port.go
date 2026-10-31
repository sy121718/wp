package purchasecontract

import (
	"context"

	"gorm.io/gorm"
)

type WarehouseRef struct {
	ID        string
	ProjectID string
	Code      string
	Name      string
}

type StockLine struct {
	WarehouseID string
	ProductID   string
	VariantID   string
	SKUCode     string
	Quantity    int

	CostPrice *float64
}

type StockChange struct {
	ProjectID  string
	Direction  string
	ReasonCode string
	SourceType string
	SourceRef  string
	Remark     string
	OperatorID string
	Lines      []StockLine
}

type WarehouseOption struct {
	ID        string
	Code      string
	Name      string
	IsDefault bool
}

type VariantOption struct {
	VariantID   string
	ProductID   string
	ProductName string
	SKUCode     string
	BareSKU     string
	CostPrice   *float64
}

type VariantCatalog interface {
	ListAllVariantOptions(ctx context.Context, projectID string) ([]VariantOption, error)
	BareSKUsByVariant(ctx context.Context, projectID string) (map[string]string, error)
}

type StockPort interface {
	ChangeStockTx(ctx context.Context, tx *gorm.DB, req *StockChange) (batchID string, err error)

	ResolveWarehouse(ctx context.Context, projectID, warehouseID string) (ref *WarehouseRef, err error)

	WarehouseNames(ctx context.Context, projectID string) (map[string]string, error)

	Warehouses(ctx context.Context, projectID string) ([]WarehouseOption, error)
}

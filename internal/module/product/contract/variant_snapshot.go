package productcontract

import "context"

type VariantSnapshot struct {
	VariantID    string
	ProductID    string
	ProjectID    string
	ProductName  string
	VariantLabel string
	SKU          string
	Price        int64

	CostPrice *int64
	Enabled   bool
}

type VariantSnapshotPort interface {
	VariantSnapshots(ctx context.Context, variantIDs []string, projectID string) (list []*VariantSnapshot, err error)
}

type VariantSnapshotByWarehousePort interface {
	VariantSnapshotsInWarehouse(ctx context.Context, refs []VariantWarehouseRef, projectID string) (list []*VariantSnapshot, err error)
}

type VariantWarehouseRef struct {
	VariantID   string
	WarehouseID string
}

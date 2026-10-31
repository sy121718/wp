package productcontract

import "context"

type VariantOption struct {
	VariantID   string
	ProductID   string
	SKUCode     string
	ProductName string

	CostPrice *float64
}

type VariantOptionsReader interface {
	ListAllVariantOptions(ctx context.Context, projectID string) ([]VariantOption, error)
}

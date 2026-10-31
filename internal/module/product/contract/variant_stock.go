package productcontract

import "context"

type WarehouseRef struct {
	ID        string
	ProjectID string
	Code      string
	Name      string
}

type VariantAvailabilityPort interface {
	AvailableQuantities(ctx context.Context, projectID string, variantIDs []string) (out map[string]int, err error)
}

type VariantAvailabilityLookupPort interface {
	VariantAvailabilities(ctx context.Context, projectID string, variantIDs []string) (map[string]int, error)
}

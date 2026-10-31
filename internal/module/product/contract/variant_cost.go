package productcontract

import "context"

type VariantCostPort interface {
	UpdateVariantCost(ctx context.Context, projectID, variantID string, cost float64, operatorID string) (err error)
}

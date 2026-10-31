package projectcontract

import (
	"context"

	projectdto "go_wp/internal/module/project/dto"
)

type ShippingPolicy = projectdto.ShippingPolicy

type ShippingPolicyReader interface {
	ShippingPolicyOf(ctx context.Context, projectID string) (policy ShippingPolicy, err error)
}

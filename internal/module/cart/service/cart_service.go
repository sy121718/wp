package cartservice

import (
	cartcontract "go_wp/internal/module/cart/contract"
	membershipcontract "go_wp/internal/module/membership/contract"
	ordercontract "go_wp/internal/module/order/contract"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

type Service struct {
	orders  ordercontract.OrderService
	product productcontract.VariantSnapshotPort

	availability productcontract.VariantAvailabilityLookupPort

	pay cartcontract.PaymentGateway

	membership membershipcontract.Reader

	shippingPolicy projectcontract.ShippingPolicyReader

	codec cookieCodec
}

func NewService(
	orders ordercontract.OrderService,
	product productcontract.VariantSnapshotPort,
	availability productcontract.VariantAvailabilityLookupPort,
	pay cartcontract.PaymentGateway,
	secret string,
) *Service {
	return &Service{
		orders:       orders,
		product:      product,
		availability: availability,
		pay:          pay,
		codec:        cookieCodec{secret: []byte(secret)},
	}
}

var _ cartcontract.CartService = (*Service)(nil)

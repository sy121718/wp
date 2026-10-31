package ordercontract

import (
	"context"

	orderdto "go_wp/internal/module/order/dto"
)

type OrderWriter interface {
	CreateAdminOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error)
}

package ordercontract

import (
	"context"

	orderdto "go_wp/internal/module/order/dto"
)

type CurrencyService interface {
	GetRate(ctx context.Context, req *orderdto.GetRateReq) (res *orderdto.RateResp, err error)

	SetRate(ctx context.Context, req *orderdto.SetRateReq) (res *orderdto.RateResp, err error)
}

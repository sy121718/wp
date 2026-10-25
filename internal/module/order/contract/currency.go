package ordercontract

// currency.go — 汇率读写契约（迁移 630，W5）。

import (
	"context"

	orderdto "go_wp/internal/module/order/dto"
)

// CurrencyService 汇率读写。
//
// 覆盖式：同一币对只留最新一条 —— 历史由**订单上的快照**保存，不靠汇率表存历史。
// 汇率用文本形式的 decimal 传（numeric(18,8)）：读成 float64 会丢精度，而汇率是财务口径。
type CurrencyService interface {
	// GetRate 取某币对的当前汇率；还没配时返回 Rate="" （「未固化」，不是错误）。
	GetRate(ctx context.Context, req *orderdto.GetRateReq) (res *orderdto.RateResp, err error)
	// SetRate 覆盖式写汇率（正数；同一币对只留最新一条）。
	SetRate(ctx context.Context, req *orderdto.SetRateReq) (res *orderdto.RateResp, err error)
}

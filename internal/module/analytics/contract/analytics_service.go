package analyticscontract

import (
	"context"

	analyticsdto "go_wp/internal/module/analytics/dto"
)

type (
	CollectReq  = analyticsdto.CollectReq
	SummaryReq  = analyticsdto.SummaryReq
	SummaryResp = analyticsdto.SummaryResp
	DailyCount  = analyticsdto.DailyCount
	PathCount   = analyticsdto.PathCount
	RankCount   = analyticsdto.RankCount
)

type AnalyticsService interface {
	Collect(ctx context.Context, req *analyticsdto.CollectReq) (err error)

	Summary(ctx context.Context, req *analyticsdto.SummaryReq) (res *analyticsdto.SummaryResp, err error)
}

type TrafficReader interface {
	Summary(ctx context.Context, req *analyticsdto.SummaryReq) (res *analyticsdto.SummaryResp, err error)
}

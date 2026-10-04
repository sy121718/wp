package orderservice

// order_status_counts.go — 各状态订单条数（概览页「待处理」卡 / AI 工具）。
//
// 「待处理」不是一个数据库字段，是几个状态的组合 —— 由本层解释一次并写进响应字段。
// 若让页面与 AI 各自去猜「待处理 = pending 还是 pending+paid」，同一页上会出现两个
// 都叫「待处理」但大小不同的数字，而且两边都不会报错。

import (
	"context"
	"errors"
	"strings"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// StatusCounts 取各状态的订单条数（**不带时间窗**：回答的是「现在有多少单等着处理」）。
func (s *Service) StatusCounts(ctx context.Context, req *orderdto.OrderStatusCountsReq) (res *orderdto.OrderStatusCountsResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	counts, err := s.orders.CountByStatus(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	// Counts 只含出现过的状态；空结果要归一成空 map 而不是 nil，
	// 否则序列化出来是 `null`，页面按对象读会多一处分支。
	if counts == nil {
		counts = map[string]int64{}
	}
	var total int64
	for _, n := range counts {
		total += n
	}
	return &orderdto.OrderStatusCountsResp{
		ProjectID: req.ProjectID,
		Counts:    counts,
		// 待付款：客户已下单还没付钱。
		PendingCount: counts[ordermodel.OrderStatusPending],
		// 待发货：钱到了但货还没发出（这才是有活要干的那些单）。
		ShipPendingCount: counts[ordermodel.OrderStatusPaid],
		TotalCount:       total,
	}, nil
}

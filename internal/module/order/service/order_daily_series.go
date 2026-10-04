package orderservice

// order_daily_series.go — 订单的按天趋势（概览页柱图 / AI 工具）。
//
// 这一层做两件事：把日期串归一化成半开窗口（与区间摘要同一个 normalizeRangeWindow），
// 以及**把空天补齐**。补零放这里而不是页面：页面要自己再算一遍「区间里有哪几天」，
// 那正是时区与边界最容易分叉的一步（SQL 里补零则要 generate_series，见 model 的说明）。

import (
	"context"
	"errors"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/utils"
)

// DailySeries 取区间内逐日连续的订单数据（含没有订单的那些天，值为 0）。
func (s *Service) DailySeries(ctx context.Context, req *orderdto.OrderDailySeriesReq) (res *orderdto.OrderDailySeriesResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	rows, err := s.orders.DailyByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	return &orderdto.OrderDailySeriesResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),
		// to 是半开上界（次日零点），减一天才是用户看到的结束日。
		To:     to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Points: fillDailyPoints(from, to, rows),
	}, nil
}

// fillDailyPoints 把「有数据的那些天」铺成区间内每天一个点（升序）。
//
// 日期键用行上的 Day 直接格式化，**不做时区换算**：PG 的 date 经驱动回来时已经是那一天的零点
// （UTC），再 .UTC() 一次在非 UTC 环境里反而可能整体挪一天。
// 循环用的 from 是 normalizeRangeWindow 给的 UTC 日界，两边键的生成方式天然一致。
func fillDailyPoints(from, to time.Time, rows []ordermodel.OrderDailyPoint) []orderdto.OrderDailyPointDTO {
	byDay := make(map[string]ordermodel.OrderDailyPoint, len(rows))
	for _, r := range rows {
		byDay[r.Day.Format(utils.LayoutDay)] = r
	}
	points := make([]orderdto.OrderDailyPointDTO, 0, maxRangeDays)
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		key := d.Format(utils.LayoutDay)
		row := byDay[key]
		points = append(points, orderdto.OrderDailyPointDTO{
			Day:            key,
			OrderCount:     row.OrderCount,
			PaidOrderCount: row.PaidOrderCount,
			NetSales:       row.NetSales,
			NetSalesLabel:  centsToYuanLabel(row.NetSales),
		})
	}
	return points
}

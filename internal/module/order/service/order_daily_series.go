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
// 桶键统一 `.UTC()` 后再格式化：**循环生成的键**来自 normalizeRangeWindow 给的
// UTC 日界，而**行上的键**来自 PG —— 两者位置不一致时 Format 出来的字符串也不同
// （同一个时刻，UTC 位置格式化成 `05:00`、本地位置格式化成 `13:00`），map 查找
// 静默落空、那一格变 0。`::date` 走驱动回来是 UTC 零点，`.UTC()` 是恒等变换；
// 但类型一换成 timestamp（按小时那侧）驱动就会贴本地时区，所以这条对两个函数
// 一视同仁地写上，不要靠「这个类型应该是 UTC」的假设。
func fillDailyPoints(from, to time.Time, rows []ordermodel.OrderDailyPoint) []orderdto.OrderDailyPointDTO {
	byDay := make(map[string]ordermodel.OrderDailyPoint, len(rows))
	for _, r := range rows {
		byDay[r.Day.UTC().Format(utils.LayoutDay)] = r
	}
	points := make([]orderdto.OrderDailyPointDTO, 0, maxRangeDays)
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		key := d.UTC().Format(utils.LayoutDay)
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

// HourlySeries 取区间内逐小时连续的订单数据（含没有订单的那些小时，值为 0）。
//
// 与 DailySeries 的关系：同一个窗口、同一套口径，只有桶的宽度不同。放在同一个文件里
// 是因为它们是同一条展示需求的两种粒度 —— 任何一处口径改动都要同时落到两边，
// 拆到两个文件只会让它更晚被发现（症状是切换粒度后柱子加起来对不上 KPI）。
//
// 上界 maxHourlyRangeDays：一天的按小时序列是 24 个桶（形状读得出来），
// 30 天就是 720 个（柱宽不足 1px，标签也放不下 —— 那是按天/按周的粒度该干的事）。
// 这里**报错而不是自动降级**：静默换粒度会让调用方以为拿到的是小时数据。
func (s *Service) HourlySeries(ctx context.Context, req *orderdto.OrderDailySeriesReq) (res *orderdto.OrderDailySeriesResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	if to.Sub(from) > time.Duration(maxHourlyRangeDays)*24*time.Hour {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	rows, err := s.orders.HourlyByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	return &orderdto.OrderDailySeriesResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),
		To:        to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Points:    fillHourlyPoints(from, to, rows),
	}, nil
}

// fillHourlyPoints 把「有数据的那些小时」铺成区间内每小时一个点（升序）。
//
// 两侧的桶键都要 `.UTC()`（理由见 fillDailyPoints）：按小时的聚合列是
// `date_trunc(...)` 返回的 **timestamp**，驱动读 `time.Time` 时按本地时区贴位置 ——
// 本机（+08）拿到的是 `13:00+08`，格式化出来是 `13:00`，而循环生成的
// UTC 键是 `05:00`，两边对不上 → **那一小时的订单静默变 0**。
func fillHourlyPoints(from, to time.Time, rows []ordermodel.OrderDailyPoint) []orderdto.OrderDailyPointDTO {
	byHour := make(map[string]ordermodel.OrderDailyPoint, len(rows))
	for _, r := range rows {
		byHour[r.Day.UTC().Format(utils.LayoutHour)] = r
	}
	points := make([]orderdto.OrderDailyPointDTO, 0, maxHourlyRangeDays*24)
	for h := from; h.Before(to); h = h.Add(time.Hour) {
		key := h.UTC().Format(utils.LayoutHour)
		row := byHour[key]
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

package orderservice

// order_range_summary.go — 区间订单摘要（概览页 KPI 与只读聚合的取数口）。
//
// 这一层只做两件事：**把日期串归一化成半开窗口**、**把分换算成展示金额**。
// 口径（哪些状态算钱、净额怎么算）全在 model 的同名查询里，这里不复制第二份。

import (
	"context"
	"errors"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/utils"
)

// maxRangeDays 区间跨度上限（天）。
//
// 与 analytics 的 maxRangeDays 同值：两个模块对「一次能查多长」给不同上限的失败模式是
// 「概览页能给 400 天的窗口、访问统计页对同样的窗口报错」，而两边都不算错，只是口径不一。
// 收口成一份需要跨模块依赖（order 不该依赖 analytics），所以这里显式声明并在注释里对齐。
const maxRangeDays = 366

// maxHourlyRangeDays 按小时取趋势的区间上限（含首尾）。
//
// 一天的按小时序列是 24 个桶（形状读得出来），30 天就是 720 个 —— 柱宽不足 1px、
// 日期标签也放不下，那已经是按天 / 按周的粒度该干的事。超限时 HourlySeries 返回
// 参数错误（而不是自动降级）：静默换粒度会让调用方以为拿到的是小时数据。
const maxHourlyRangeDays = 2

// SummaryByRange 取区间订单摘要（概览页 KPI 与只读聚合共用）。
//
// 错误只回 enums 里的可翻译 key：本模块的响应文案统一由 XxxFacingMessages 白名单收口，
// 这里直接拼中文会让「后台页面禁止直出内部错误」那条约定出现一个漏口。
func (s *Service) SummaryByRange(ctx context.Context, req *orderdto.OrderRangeSummaryReq) (res *orderdto.OrderRangeSummaryResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	row, err := s.orders.SummaryByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	return &orderdto.OrderRangeSummaryResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),
		// to 是半开上界（次日零点），减一天才是用户看到的「结束日」。
		To:             to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		OrderCount:     row.OrderCount,
		PaidOrderCount: row.PaidOrderCount,
		NetSales:       row.NetSales,
		NetSalesLabel:  centsToYuanLabel(row.NetSales),
	}, nil
}

// normalizeRangeWindow 把两个日期串归一化成半开窗口 [from, to)（UTC 日界）。
//
// 规则：
//   - 两个参数都必须是 YYYY-MM-DD，任一为空或非法 → ErrInvalidParam
//     （不给「静默默认窗口」：概览页永远显式传区间，默认值会让「我看到的是哪一段」说不清）；
//   - 上界晚于明天 → 收敛到明天（未来的日期没有数据，也不该被当成合法窗口）；
//   - from 晚于 to → ErrInvalidParam；跨度超过 maxRangeDays → ErrInvalidParam。
//
// **按 UTC 日界**而不是本地时区：全站的按天聚合都是 UTC 的 day 桶
// （analytics 的按天统计就是），本地时区会让「今天的订单数」与「今天那根柱子」
// 在同一个页面上错开一个时区的量。
//
// now 由调用方传入而不是内部取 time.Now()：窗口收敛规则要能被测试钉住，
// 否则「未来日期收敛到今天」这条只能靠等一天来验证。
func normalizeRangeWindow(rawFrom, rawTo string, now time.Time) (from, to time.Time, err error) {
	dayFrom, okFrom := parseDay(rawFrom)
	dayTo, okTo := parseDay(rawTo)
	if !okFrom || !okTo {
		return time.Time{}, time.Time{}, errors.New(orderenums.ErrInvalidParam)
	}
	from, to = dayFrom, dayTo.AddDate(0, 0, 1)
	if tomorrow := dayStart(now).AddDate(0, 0, 1); to.After(tomorrow) {
		to = tomorrow
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, errors.New(orderenums.ErrInvalidParam)
	}
	if to.Sub(from) > time.Duration(maxRangeDays)*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New(orderenums.ErrInvalidParam)
	}
	return from, to, nil
}

// parseDay 解析 YYYY-MM-DD（UTC 日零点；空 / 非法返回 ok=false）。
//
// analytics service 有一份同名的私有实现（analytics_query.go 的 parseDay）——
// 两份是刻意的：两个模块对「区间」的业务定义将来会分叉（订单能查任意历史，
// 统计有保留期），共用一份会把它们绑在一起。真要收口，落点应是 pkg/utils 而不是互相 import。
func parseDay(raw string) (t time.Time, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation(utils.LayoutDay, raw, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// dayStart 取某个时刻所在 UTC 日的零点。
func dayStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

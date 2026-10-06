package orderservice

// order_sales_overview.go — 「销售概览」页的用例层（订单模块 /admin/orders/overview）。
//
// 这一层只做四件事，一件都不往外推：
//  1. **归一化窗口**（复用 normalizeRangeWindow，与「区间订单摘要」同一套规则）；
//  2. **把请求里的筛选文案翻译成 model 认识的状态名单**（白名单在这里，不在 model）；
//  3. **算派生值**（AOV / ACV / 平均每单 / 平均每件 / 环比变化率）；
//  4. **给月度趋势补零**（区间里有哪几个月依赖时区与边界，最容易分叉的一步）。
//
// 事实全在 model 的两条查询里（口径不在这里复制第二份），展示串全在 inbound/http
// 的视图层（金额换算只有一处）。本层不 import gin、不 import model 的 SQL 常量。

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/utils"
)

const (
	// salesDefaultMonthly 趋势默认回看的月数（半年 —— 足以看出季节性，又不至于挤成细柱）。
	salesDefaultMonthly = 6
	// salesMaxMonthly 趋势回看月数上限（两年）。上限存在的理由是布局：24 根柱在一张
	// 卡里已经只有十几像素宽，再多就只能靠 tooltip 读出数字了。
	salesMaxMonthly = 24
)

// salesStatusWhitelist 销售概览允许筛选的订单状态。
//
// **刻意不含 cancelled / refunded**：本页每个数字都在回答「卖了多少钱」，
// 把取消单算进来得到一个没人要的口径（那属于「下过多少单」，订单列表页已经在回答）。
// 白名单在 service 而不是 model：model 只执行给定的名单，是否允许是这个用例的决定。
var salesStatusWhitelist = []string{
	ordermodel.OrderStatusPaid,
	ordermodel.OrderStatusShipped,
	ordermodel.OrderStatusCompleted,
}

// SalesOverview 取销售概览（卡片 + 客户 + 环比 + 月度趋势）。
func (s *Service) SalesOverview(ctx context.Context, req *orderdto.SalesOverviewReq) (res *orderdto.SalesOverviewResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	statuses, statusText, err := salesStatuses(req.Status)
	if err != nil {
		return nil, err
	}
	monthly := salesMonthlyMonths(req.Monthly)
	filter := ordermodel.OrderSalesFilter{Statuses: statuses}

	// ① 区间内的事实。
	row, err := s.orders.SalesOverviewByRange(ctx, req.ProjectID, from, to, filter)
	if err != nil {
		return nil, err
	}

	// ② 客户维度：新客 / 回头客 / 复购复用既有的客户增长口径（orderCustomerCTEs 是那个口径的
	// 唯一真源）。它**不吃状态筛选** —— 那边的「客户」定义是「区间内下过单的人」，
	// 与销售数字的「计入消费的状态」本来就是两个问题，硬绑在一起会让「新客 3 人」
	// 随销售状态筛选变化，而一个人是不是新客与订单状态无关。
	growth, err := s.orders.CustomerGrowthByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}

	resp := &orderdto.SalesOverviewResp{
		ProjectID:          req.ProjectID,
		From:               from.Format(utils.LayoutDay),
		To:                 to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Status:             statusText,
		Monthly:            monthly,
		OrderCount:         row.OrderCount,
		ItemRows:           row.ItemRows,
		Units:              row.Units,
		Sales:              row.Sales,
		AvgOrderValue:      ratioCents(row.Sales, row.OrderCount),
		AvgCustomerValue:   ratioCents(row.Sales, row.Customers),
		AvgItemsPerOrder:   ratioFloat(row.ItemRows, row.OrderCount),
		AvgSalesPerUnit:    ratioCents(row.Sales, row.Units),
		Customers:          row.Customers,
		NewCustomers:       growth.NewCustomers,
		ReturningCustomers: growth.ReturningCustomers,
		Repurchasers:       growth.Repurchasers,
		RepurchaseRatePct:  repurchaseRatePct(growth.NewRepurchasers, growth.ReturningCustomers, growth.OrderingCustomers),
	}

	// ③ 环比：紧邻的上一段**等长**区间。等长是关键 —— 拿「本月 1 号到今天」与「上月整月」
	// 比，月初几天的报表永远显示暴跌，那个数字没有信息量。
	if cmp := s.salesCompare(ctx, req.ProjectID, from, to, filter, row); cmp != nil {
		resp.Compare = cmp
	}

	// ④ 月度趋势（固定回看窗口，与卡片的筛选区间不同口径 —— 见 dto 的说明）。
	points, err := s.salesMonthlyPoints(ctx, req.ProjectID, monthly, filter)
	if err != nil {
		return nil, err
	}
	resp.MonthlyPoints = points
	return resp, nil
}

// salesStatuses 把请求里的状态文案翻译成 model 的状态名单。
//
// 返回的第二个值是**生效的文案**（用于回显）：请求给了非法值时按「没给」处理
// （回落全量），**不报错** —— 一个拼错的状态名不值得让整张页面打不开，
// 而回显生效值让「我明明筛了已发货，怎么数字没变」变成看得见的答案。
//
// 三种写法都收：`paid,shipped` / `paid shipped` / `PAID`。
func salesStatuses(raw string) (statuses []string, effective string, err error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return nil, "", nil
	}
	allowed := make(map[string]bool, len(salesStatusWhitelist))
	for _, st := range salesStatusWhitelist {
		allowed[st] = true
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '，'
	})
	seen := make(map[string]bool, len(fields))
	picked := make([]string, 0, len(fields))
	for _, f := range fields {
		if !allowed[f] || seen[f] {
			continue
		}
		seen[f] = true
		picked = append(picked, f)
	}
	if len(picked) == 0 {
		// 全是非法状态名 → 当没筛（而不是返回 0 条，那会让整页看起来「这段时间没生意」）。
		return nil, "", nil
	}
	return picked, strings.Join(picked, ","), nil
}

// salesMonthlyMonths 生效的趋势回看月数（越界回落默认值，不报错 —— 与状态筛选同一条理由）。
func salesMonthlyMonths(raw int) int {
	if raw <= 0 {
		return salesDefaultMonthly
	}
	if raw > salesMaxMonthly {
		return salesMaxMonthly
	}
	return raw
}

// salesCompare 取上一段等长区间的事实并算变化率；上一期完全没数据时返回 nil。
//
// 「完全没数据」的判据是三个数全为 0（而不是只看订单数）：上一期可能只有游客单
// （订单数 > 0 但客户数 0），那时仍应显示订单数与销售额的对比。
func (s *Service) salesCompare(ctx context.Context, projectID string, from, to time.Time, f ordermodel.OrderSalesFilter, cur ordermodel.OrderSalesOverviewRow) *orderdto.SalesCompareDTO {
	span := to.Sub(from)
	prevTo := from
	prevFrom := from.Add(-span)
	// 上一期窗口同样受 maxRangeDays 约束：等长区间不会超限，但 from 可能早到
	// 平台上线之前 —— 那没关系，查出来就是 0 行。
	prev, err := s.orders.SalesOverviewByRange(ctx, projectID, prevFrom, prevTo, f)
	if err != nil {
		return nil
	}
	if prev.OrderCount == 0 && prev.Sales == 0 && prev.Customers == 0 {
		return nil
	}
	return &orderdto.SalesCompareDTO{
		From:                prevFrom.Format(utils.LayoutDay),
		To:                  prevTo.AddDate(0, 0, -1).Format(utils.LayoutDay),
		OrderCount:          prev.OrderCount,
		Sales:               prev.Sales,
		Customers:           prev.Customers,
		OrderCountChangePct: changePct(prev.OrderCount, cur.OrderCount),
		SalesChangePct:      changePct(prev.Sales, cur.Sales),
		CustomersChangePct:  changePct(prev.Customers, cur.Customers),
	}
}

// salesMonthlyPoints 取近 months 个月（含当月）的逐月趋势，缺失的月补零。
//
// 窗口是**固定回看**（当月 1 号往前推 months-1 个月），与筛选区间无关 ——
// 趋势图的用途是「近半年卖得怎么样」，不是「我选的这两天里每个月卖得怎么样」。
//
// 桶键用 UTC 月首：model 的查询已经 `(date_trunc('month', ...))::date`，
// 这里只按同一把尺子补零（AGENTS.md「桶键读回来必须还是同一个时刻」）。
func (s *Service) salesMonthlyPoints(ctx context.Context, projectID string, months int, f ordermodel.OrderSalesFilter) ([]orderdto.SalesMonthlyPointDTO, error) {
	now := time.Now().UTC()
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	start := thisMonth.AddDate(0, -(months - 1), 0)
	end := thisMonth.AddDate(0, 1, 0)

	rows, err := s.orders.SalesMonthlyByRange(ctx, projectID, start, end, f)
	if err != nil {
		return nil, err
	}
	byMonth := make(map[string]ordermodel.OrderSalesMonthlyRow, len(rows))
	for _, r := range rows {
		byMonth[r.Month.UTC().Format("2006-01")] = r
	}
	points := make([]orderdto.SalesMonthlyPointDTO, 0, months)
	for i := 0; i < months; i++ {
		key := start.AddDate(0, i, 0).Format("2006-01")
		p := orderdto.SalesMonthlyPointDTO{Month: key}
		if r, ok := byMonth[key]; ok {
			p.OrderCount = r.OrderCount
			p.Sales = r.Sales
			p.Customers = r.Customers
		}
		points = append(points, p)
	}
	return points, nil
}

// ratioCents 整数分相除（分母为 0 时返回 0）。
//
// 0 而不是报错：分母为 0 意味着「这段时间没有订单 / 没有客户」，页面上就是 0.00 ——
// 与「平均 0 元」是同一件事，而 NaN 会让模板渲染出「NaN」并且没有任何报错。
func ratioCents(numerator, denominator int64) int64 {
	if denominator <= 0 {
		return 0
	}
	return int64(math.Round(float64(numerator) / float64(denominator)))
}

// ratioFloat 两个计数相除，保留两位小数。
func ratioFloat(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return math.Round(float64(numerator)*100/float64(denominator)) / 100
}

// changePct 变化率（百分比，一位小数）。
//
// prev=0（上一期为 0）时返回 nil：**不是 0%**。「从 0 涨到 100」的变化率是
// 无穷大而不是 100%，而写 0% 会让页面显示「持平」—— 那是一个明确错误的结论。
// 调用方拿到 nil 时整块不显示变化率，只显示绝对值。
func changePct(prev, cur int64) *float64 {
	if prev == 0 {
		return nil
	}
	v := math.Round(float64(cur-prev)*1000/float64(prev)) / 10
	return &v
}

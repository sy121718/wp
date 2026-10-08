package orderservice

//
// 这一层只做两件事：归一化窗口、把 model 的行摊成 DTO。
// 口径（哪些单计入消费、商品件数怎么数）全在 model 的 orderSoldQuantitySQL 里。

//
// 「待处理」不是一个数据库字段，是几个状态的组合 —— 由本层解释一次并写进响应字段。
// 若让页面与 AI 各自去猜「待处理 = pending 还是 pending+paid」，同一页上会出现两个
// 都叫「待处理」但大小不同的数字，而且两边都不会报错。

//
// 这一层只做三件事：归一化窗口、把 limit 收进合法区间、给每行写名次。
// 排序规则（销量优先、金额与商品 id 收尾）全在 model 的 SQL 里 —— 名次必须是那条
// ORDER BY 的产物，在这里重排一次就等于把排序规则写了第二遍。

//
// 这一层只做两件事：**把日期串归一化成半开窗口**、**把分换算成展示金额**。
// 口径（哪些状态算钱、净额怎么算）全在 model 的同名查询里，这里不复制第二份。

//
// 这一层做两件事：把日期串归一化成半开窗口（与区间摘要同一个 normalizeRangeWindow），
// 以及**把空天补齐**。补零放这里而不是页面：页面要自己再算一遍「区间里有哪几天」，
// 那正是时区与边界最容易分叉的一步（SQL 里补零则要 generate_series，见 model 的说明）。

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

// SoldQuantityByRange 取区间内的商品销售总量（件数）。
//
// 与 SummaryByRange 分开而不是塞进它：那条 SQL 只碰 orders 表，本条要 join order_items。
// 合成一条的代价是概览页每次取 KPI 都得付一次 join，而「件数」这一格多数时候不看；
// 两条查询各自可解释、各自可用索引，比一条又长又贵的合并查询更划算。
func (s *Service) SoldQuantityByRange(ctx context.Context, req *orderdto.OrderSoldQuantityReq) (res *orderdto.OrderSoldQuantityResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	row, err := s.orders.SoldQuantityByRange(ctx, req.ProjectID, from, to)
	if err != nil {
		return nil, err
	}
	return &orderdto.OrderSoldQuantityResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),
		// to 是半开上界（次日零点），减一天才是用户看到的「结束日」（同 SummaryByRange）。
		To:         to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Quantity:   row.Quantity,
		OrderCount: row.OrderCount,
	}, nil
}

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

// defaultTopProducts 概览页默认看几条。
//
// 定成 5 是因为榜单块的高度固定：条数一多卡片就被撑开，页面整体版式随之跳动。
// AI 工具可以显式传更大值（上限见 model.MaxTopProductLimit）。
const defaultTopProducts = 5

// TopProducts 取区间内销量最高的若干商品（含名次，已按销量降序）。
func (s *Service) TopProducts(ctx context.Context, req *orderdto.OrderTopProductsReq) (res *orderdto.OrderTopProductsResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	from, to, err := normalizeRangeWindow(req.From, req.To, time.Now())
	if err != nil {
		return nil, err
	}
	// limit 在这里 clamp 而不是报错：调用方多半是页面或模型，它填 1000 的意思是
	// 「尽量多」，不是「我要一个 1000 行的答案」（model 里还会再 clamp 一次作为兜底）。
	limit := req.Limit
	if limit <= 0 {
		limit = defaultTopProducts
	}
	if limit > ordermodel.MaxTopProductLimit {
		limit = ordermodel.MaxTopProductLimit
	}
	rows, err := s.orders.TopProductsByRange(ctx, req.ProjectID, from, to, limit)
	if err != nil {
		return nil, err
	}
	items := make([]orderdto.OrderTopProductItemDTO, 0, len(rows))
	for i, row := range rows {
		items = append(items, orderdto.OrderTopProductItemDTO{
			Rank:        i + 1,
			ProductID:   row.ProductID,
			ProductName: row.ProductName,
			SKU:         row.SKU,
			Quantity:    row.Quantity,
			Amount:      row.Amount,
			AmountLabel: centsToYuanLabel(row.Amount),
		})
	}
	return &orderdto.OrderTopProductsResp{
		ProjectID: req.ProjectID,
		From:      from.Format(utils.LayoutDay),
		To:        to.AddDate(0, 0, -1).Format(utils.LayoutDay),
		Limit:     limit,
		Items:     items,
	}, nil
}

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
		Currency:           orderCurrency(),
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
			p.NewOrderCount = r.NewOrderCount
			p.ReturningOrderCount = r.ReturningOrderCount
			p.GuestOrderCount = r.GuestOrderCount
			p.NewSales = r.NewSales
			p.ReturningSales = r.ReturningSales
			p.GuestSales = r.GuestSales
			p.NewCustomers = r.NewCustomers
			p.ReturningCustomers = r.ReturningCustomers
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

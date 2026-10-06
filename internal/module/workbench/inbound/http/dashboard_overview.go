package workbenchhttp

// dashboard_overview.go — 概览页的跨模块取数（订单 / 访问统计 / 页面类型）。
//
// 为什么组装点在 workbench：概览页是唯一需要同时看这几个模块的地方，而模块之间不能
// 互相依赖（表隔离：analytics 读不到 orders，order 读不到 pages）。workbench 是它们
// 共同的消费者，所以由它把各自的只读结论拼起来 —— 各模块只回答自己领域的问题，
// 不参与对方的计算。
//
// 时间口径：**UTC 日界**，与 order 的按天桶、analytics 的按天聚合逐字一致
//（analytics: `(viewed_at AT TIME ZONE 'UTC')::date`）。用本地日界会让「今日 KPI」
// 与柱图最后一根柱子算出两个数，而两者都会被当成对的。

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	analyticsdto "go_wp/internal/module/analytics/dto"
	orderdto "go_wp/internal/module/order/dto"
	pageenums "go_wp/internal/module/page/enums"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	// overviewTopLimit 热销榜条数（口径「热销商品前十」，见 docs/17 §P6）。
	overviewTopLimit = 10
	// overviewPageTopLimit 页面排行榜展示几条（口径「页面前十」，见 docs/17 §P6）。
	overviewPageTopLimit = 10
	// overviewPathLimit 取路径排行时一次要多少条。
	//
	// 文章浏览量 = 「路径排行里的文章页之和」，所以要够大才不漏：取 200（analytics 的上限）。
	// 仍有理论上限：某站点的文章页若都在 200 名之外，这个数字会偏小 —— 取舍是
	// 「多取一分就多一分查询成本」，而概览页每一秒都在被访问。
	overviewPathLimit = 200
)

// OverviewOrderPort 概览页所需的订单只读聚合（最窄面）。
//
// 只列概览真正会调的四个方法：把整个 OrderService 交进来，会让「概览顺手改个订单状态」
// 从「需要显式加一个方法」退化成「随手就能写」—— 能力边界该由接口形状决定。
type OverviewOrderPort interface {
	SummaryByRange(ctx context.Context, req *orderdto.OrderRangeSummaryReq) (res *orderdto.OrderRangeSummaryResp, err error)
	DailySeries(ctx context.Context, req *orderdto.OrderDailySeriesReq) (res *orderdto.OrderDailySeriesResp, err error)
	// HourlySeries 按小时的趋势（粒度切到「小时」时走它）。
	HourlySeries(ctx context.Context, req *orderdto.OrderDailySeriesReq) (res *orderdto.OrderDailySeriesResp, err error)
	TopProducts(ctx context.Context, req *orderdto.OrderTopProductsReq) (res *orderdto.OrderTopProductsResp, err error)
	StatusCounts(ctx context.Context, req *orderdto.OrderStatusCountsReq) (res *orderdto.OrderStatusCountsResp, err error)
	SoldQuantityByRange(ctx context.Context, req *orderdto.OrderSoldQuantityReq) (res *orderdto.OrderSoldQuantityResp, err error)
	// CustomerGrowthByRange 区间内的新客数（客户域的唯一一格）。
	//
	// 放在这个接口里而不是新开一个端口：它同样是订单模块的只读聚合 ——
	// 「谁是新人」只有看得到 orders 表的那一侧答得出来（用户模块手里只有注册时间，
	// 按注册时间算出来的「新客」与这里的口径会是两个数）。
	CustomerGrowthByRange(ctx context.Context, req *orderdto.CustomerGrowthReq) (res *orderdto.CustomerGrowthResp, err error)
}

// OverviewAnalyticsPort 概览页所需的访问统计只读面（一条方法）。
type OverviewAnalyticsPort interface {
	Summary(ctx context.Context, req *analyticsdto.SummaryReq) (res *analyticsdto.SummaryResp, err error)
}

// OverviewPageKindPort 路径 → 页面类型（判断哪些浏览发生在文章页上）。
type OverviewPageKindPort interface {
	KindsOfPaths(ctx context.Context, projectID string, paths []string) (map[string]string, error)
}

// OverviewCurrencyPort 货币符号来源（后台数据字典的 currency 类型）。
//
// 符号的**唯一来源是那张字典表**（sys_dict，运营可增删货币），不要在代码里
// 另建一份代码→符号的映射：两份会漂移，而漂移的表现是「后台加了港币、
// 概览页显示的仍是三字母代码」，不报错、也没人知道该改哪一处。
type OverviewCurrencyPort interface {
	ListDictOptions(ctx context.Context, dictType string) ([]sysconfigdto.DictOption, error)
}

// overviewKPI KPI 卡片的数字（除「待发货 / 待付款」两个状态计数外，全部为**当前区间**口径）。
type overviewKPI struct {
	// RangeOrders / RangeSalesCents 是**当前区间**的口径（不再是「今日」）。
	//
	// 字段名跟着语义走：区间可以是一天、一周、一年，叫 Today 的名字会在下一次改动里
	// 把某个人骗一次 —— 他会在「本月」的窗口上按「今日订单」去解释这个数字。
	RangeOrders     int64
	RangeSalesCents int64
	RangeSalesLabel string
	// RangeItems 区间内售出的商品总件数（只算计入消费的订单，与热销榜同口径）。
	//
	// 与「净销售额」并列时两者回答的是不同问题：件数看规模、金额看收入。
	// 它们对不上是正常的（打折、赠品、退款），所以两格各有自己的标签。
	RangeItems int64
	// PageViews 区间内全站页面浏览总量（PV，全部路径）。
	PageViews int64
	// ArticleViews 其中发生在文章页上的那部分。
	//
	// 只算文章页要问 page 模块「这个路径是不是文章页」（表隔离：analytics 只认 path），
	// 且路径排行只取前 N 条 —— 所以它是**下界**，不是精确值。页面上的小字注明口径。
	ArticleViews int64
	// NewCustomers 区间内的新客数（首次下单落在这个窗口里的人数）。
	//
	// 与「区间下单客户」不是同一格：那个数在客户概览页上（那里的分母是它）。
	// 这里只放最常被问的那一个 —— 「这段时间拉来多少新人」。
	NewCustomers     int64
	ShipPendingCount int64
	PendingCount     int64
}

// overviewTrendPoint 趋势图上的一根柱子。
type overviewTrendPoint struct {
	Day string
	// DayLabel 图上显示的短日期（MM-DD）：模板里切字符串需要一个「怎么切」的约定，
	// 放在服务端切一次比在模板里切更稳（Jet 的切片语义不该由模板作者各自解释）。
	DayLabel   string
	Orders     int64
	NetSales   int64
	SalesLabel string
	// Views 该天（按周聚合时是该周）的页面浏览数（PV，全部路径）。
	Views int64
	// SalesHeightPct / ViewsHeightPct 0~100：模板不做算术，柱高在服务端算好。
	//
	// **两套柱高分开放**：同一根柱子在两张图里代表不同指标。共用一个高度字段会让
	// 「切到浏览量」时柱子仍按销售额的高低排 —— 图形看着正常，数字全错位，
	// 而这种错没有任何报错会提示。
	SalesHeightPct int
	ViewsHeightPct int
	// X / BarWidth 这根柱子在 viewBox 里的横坐标与宽度（按点数分配，见 layoutTrendBars）。
	//
	// 不给模板一个固定步长：固定步长在长区间上会把后面的柱子推出画布，
	// 而 SVG 的默认 overflow 是 hidden —— 结果是「图只画了一半」且没有任何错误。
	X        int
	BarWidth int
	// ShowLabel 是否画日期标签（标签稀疏化，见 trendLabelMax）。
	ShowLabel bool
	// LabelX / LabelAnchor 标签自身的横坐标与对齐方式（见 layoutTrendBars）。
	//
	// 与柱子的 X 分开：柱子的中心在线宽上是对的，但**首尾标签**用它做锚点会越出
	// viewBox —— 按小时 24 个桶时 step≈20px，第一个标签居中在 x≈10 而 5 个字符要
	// 占 25px，左半边被裁掉（页面上是「.7:00」这种缺半个字的标签，不报错、也不影响
	// 任何计数断言）。首尾改成 start / end 对齐后锚点落到柱子边缘，标签收回画布内。
	LabelX      int
	LabelAnchor string
}

// overviewTopProduct 榜单的一行（跨工程合并后重排名次）。
type overviewTopProduct struct {
	Rank        int
	ProductName string
	SKU         string
	Quantity    int64
	AmountLabel string
}

// overviewTopPage 页面排行的一行（跨工程合并后重排名次）。
//
// 展示的是**路径**而不是页面标题：analytics 只记 path（表隔离，读不到 pages），
// 而排行榜要的是「哪些地址被看得最多」；标题只对文章页有意义，落地页与首页没有标题。
// Kind 由 page 模块补（查不到时为空串），只作辅助列 —— 拿不到类型不影响排行本身。
type overviewTopPage struct {
	Rank  int
	Path  string
	Kind  string
	Views int64
	// KindKey 类型对应的 i18n 词条 key（空串 = 类型未知，展示层不渲染那枚标签）。
	//
	// 服务端只给 key、不给中文：后台文案一律走词条（硬编码中文会被 i18n 覆盖门禁判红），
	// 而「类型 → 文案」的映射放在 Go 里只写一次，模板不必再维护一份。
	KindKey string
}

// overviewSnapshot 概览页的全部跨模块数据。
type overviewSnapshot struct {
	// OrdersReady / AnalyticsReady 表示对应端口**已接线**（不是「有数据」）：
	// 未接线时模板渲染空态并说明「暂不可用」，而不是显示一片 0 —— 0 会被当成真实统计。
	OrdersReady    bool
	AnalyticsReady bool
	// Range 生效的区间（含首尾、粒度与是否被收敛）—— 窗口的**唯一**来源。
	//
	// 早先这里另有 Today / From / To 三个平行字段，改动后它们与 Range 重复：
	// 同一个窗口存两份，迟早出现「筛选条显示本月、标题显示上周」这种一致性问题，
	// 而两份各自看都对。窗口收成一个字段。
	Range overviewRange
	// Presets 筛选条上的一排预设按钮（URL 已拼好，模板不拼 query）。
	Presets []rangePreset
	// GranularityPresets 趋势图右上角的粒度按钮（小时 / 天 / 周 / 月）。
	GranularityPresets []granularityPreset
	KPI                overviewKPI
	Trend              []overviewTrendPoint
	Top                []overviewTopProduct
	// TopPages 页面排行（浏览量降序，跨工程合并）。
	TopPages []overviewTopPage
	// PortsReady 三个跨模块端口是否都已接线。
	//
	// 未接线时页面渲染的是一片 0，而 0 会被当成真实统计（「今天一单都没有」）——
	// 所以模板据这个字段把整个概览区换成一句「暂不可用」，而不是显示 0。
	PortsReady bool
	// FailedBlocks 本次取数失败的块名（去重，只含**模块名**，不含错误文本）。
	//
	// 不把 err.Error() 交给模板：那条路径会把它当内部错误直出，而概览页是登录后的第一跳，
	// 页面上出现 "sql: no rows in result set" 既不解决问题也暴露实现。细节走结构化日志。
	FailedBlocks []string
}

// SetOverviewPorts 注入概览页的跨模块只读端口。
//
// 用 setter 而不是加 SetupWorkbenchRoutes 的形参：那个签名已被装配层与测试多处调用，
// 加参数会波及所有调用点（而这里注入的是「概览页专有的三个可选依赖」）。
// 任一参数为 nil 表示该块不可用，页面按空态渲染（不 panic、不 500）。
func (h *Handle) SetOverviewPorts(orders OverviewOrderPort, analytics OverviewAnalyticsPort,
	pageKinds OverviewPageKindPort, currencies OverviewCurrencyPort) {
	h.overviewOrders = orders
	h.overviewAnalytics = analytics
	h.overviewPageKinds = pageKinds
	h.overviewCurrencies = currencies
}

// currencySymbol 取默认货币的符号（取不到时回落到货币代码本身）。
//
// 每次进概览页读一次字典：这张表运营可能随时改（加货币、改符号），
// 而概览页本来就在做十几次跨模块查询，多这一次可以忽略；缓存反而会让
// 「刚在后台把 ¥ 改成 ￥」这件事在界面上不生效，且不知道要等多久。
func (h *Handle) currencySymbol(ctx context.Context) string {
	code := strings.TrimSpace(i18n.GetDefaultCurrency())
	if h.overviewCurrencies == nil || code == "" {
		return code
	}
	opts, err := h.overviewCurrencies.ListDictOptions(ctx, "currency")
	if err != nil {
		// 读不到字典不是错误：金额照常显示，只是没有符号前缀。
		// 把整张卡打空比少一个符号糟得多。
		return code
	}
	for _, o := range opts {
		if !strings.EqualFold(strings.TrimSpace(o.Code), code) {
			continue
		}
		if sym := strings.TrimSpace(o.Symbol); sym != "" {
			return sym
		}
		return code
	}
	return code
}

// collectOverview 汇总全部工程的概览数据（任一模块失败只影响它自己的块）。
//
// 区间由调用方解析好传进来（见 dashboard_range.go）：KPI / 趋势 / 榜单三块必须是**同一个**
// 窗口，所以「本月」这个词只在一个地方被翻译成日期。
func (h *Handle) collectOverview(ctx context.Context, projectIDs []string, rng overviewRange) overviewSnapshot {
	snap := overviewSnapshot{Range: rng}
	byDay := h.collectOrderOverview(ctx, projectIDs, rng, &snap)
	viewsByDay := make(map[string]int64)
	viewsByPath := make(map[string]int64)
	h.collectViews(ctx, projectIDs, rng, &snap, viewsByDay, viewsByPath)
	// 趋势图在收完两块数据之后一次装配：两张图共用一根日期轴，
	// 各画各的柱子（销售额图与浏览量图），所以合并只做一次。
	snap.Trend = buildTrend(byDay, viewsByDay, rng.Granularity, h.currencySymbol(ctx))
	snap.KPI.RangeSalesLabel = moneyLabel(snap.KPI.RangeSalesCents, h.currencySymbol(ctx))
	snap.PortsReady = snap.OrdersReady && snap.AnalyticsReady
	return snap
}

// collectOrderOverview 取订单侧的 KPI / 趋势 / 榜单（逐工程取回后在内存里累加）。
//
// 逐工程而不是一次全局查询：orders 带 FORCE 策略，聚合方法都要求显式工程 id
// （那正是「不许出现不限工程的查询」这条纪律的形状）。工程数量在个位数量级，
// 多几次查询换来的是「不可能读到别人的数据」。
// 返回值是「天 → 点」的映射而不是已装配的序列：调用方要把浏览量的按天数据并进同一根
// 日期轴再装配（见 collectOverview / buildTrend），这里只负责把订单侧的事实取回来。
func (h *Handle) collectOrderOverview(ctx context.Context, projectIDs []string, rng overviewRange, snap *overviewSnapshot) map[string]*overviewTrendPoint {
	if h.overviewOrders == nil {
		return nil
	}
	snap.OrdersReady = true
	byDay := make(map[string]*overviewTrendPoint, rng.Days)
	var products []overviewTopProduct

	for _, pid := range projectIDs {
		// KPI / 趋势 / 榜单同一窗口：这里只认 rng 算好的 from/to，
		// 不接受「今日」之类的词（见 dashboard_range.go 文件头）。
		if res, err := h.overviewOrders.SummaryByRange(ctx, &orderdto.OrderRangeSummaryReq{
			ProjectID: pid, From: rng.From, To: rng.To,
		}); err != nil {
			snap.fail("order", pid, err)
		} else {
			snap.KPI.RangeOrders += res.OrderCount
			snap.KPI.RangeSalesCents += res.NetSales
		}

		// 商品销售总量：与热销榜同一个筛选条件（只算计入消费的订单），
		// 所以页面上「总量」与「榜单各项之和」必须自洽（feature 测试钉住这一点）。
		if res, err := h.overviewOrders.SoldQuantityByRange(ctx, &orderdto.OrderSoldQuantityReq{
			ProjectID: pid, From: rng.From, To: rng.To,
		}); err != nil {
			snap.fail("order", pid, err)
		} else {
			snap.KPI.RangeItems += res.Quantity
		}

		// 新客：与客户概览页同一个方法、同一个区间，两处显示的必须是同一个数。
		if res, err := h.overviewOrders.CustomerGrowthByRange(ctx, &orderdto.CustomerGrowthReq{
			ProjectID: pid, From: rng.From, To: rng.To,
		}); err != nil {
			snap.fail("order", pid, err)
		} else {
			snap.KPI.NewCustomers += res.NewCustomers
		}

		if res, err := h.overviewOrders.StatusCounts(ctx, &orderdto.OrderStatusCountsReq{ProjectID: pid}); err != nil {
			snap.fail("order", pid, err)
		} else {
			snap.KPI.ShipPendingCount += res.ShipPendingCount
			snap.KPI.PendingCount += res.PendingCount
		}

		if res, err := h.trendSeries(ctx, pid, rng); err != nil {
			snap.fail("order", pid, err)
		} else {
			for _, p := range res.Points {
				pt := byDay[p.Day]
				if pt == nil {
					pt = &overviewTrendPoint{Day: p.Day}
					byDay[p.Day] = pt
				}
				pt.Orders += p.OrderCount
				pt.NetSales += p.NetSales
			}
		}

		if res, err := h.overviewOrders.TopProducts(ctx, &orderdto.OrderTopProductsReq{
			ProjectID: pid, From: rng.From, To: rng.To, Limit: overviewTopLimit,
		}); err != nil {
			snap.fail("order", pid, err)
		} else {
			for _, it := range res.Items {
				products = append(products, overviewTopProduct{
					ProductName: it.ProductName, SKU: it.SKU,
					Quantity: it.Quantity, AmountLabel: it.AmountLabel,
				})
			}
		}
	}
	snap.Top = buildTop(products)
	return byDay
}

// trendSeries 按当前粒度取订单趋势。
//
// 只有「小时」需要向下的专门查询；天 / 周 / 月共用按天数据，合并发生在 buildTrend。
// 这样粒度切换的成本是「多一次按天查询」，而不是「每种粒度一条 SQL」——
// 后者会把「周与月的分桶规则」复制到 SQL 里，而分桶规则一改就该只改一处。
func (h *Handle) trendSeries(ctx context.Context, projectID string, rng overviewRange) (*orderdto.OrderDailySeriesResp, error) {
	req := &orderdto.OrderDailySeriesReq{ProjectID: projectID, From: rng.From, To: rng.To}
	if rng.Granularity == trendGranularityHour {
		return h.overviewOrders.HourlySeries(ctx, req)
	}
	return h.overviewOrders.DailySeries(ctx, req)
}

// collectViews 统计**区间内**的页面浏览（全站总量 + 其中文章页那部分）。
//
// 两步而非一步：analytics 只认 path，判断「这个路径是不是文章页」要靠 page 模块
// （表隔离：analytics 读不到 pages）。查不到类型的路径按**非文章页**处理 ——
// 猜一个默认值会让已下线的文章页继续被算进来，而两边都不会报错。
//
// 总量（PageViews）不依赖 page 模块：它是 analytics 直接给的 Total，
// 所以 page 端口没接线时总量照旧可用，只有「其中文章页」那半格失去意义。
func (h *Handle) collectViews(ctx context.Context, projectIDs []string, rng overviewRange, snap *overviewSnapshot, viewsByDay map[string]int64, viewsByPath map[string]int64) {
	if h.overviewAnalytics == nil {
		return
	}
	snap.AnalyticsReady = true
	// 路径 → 类型（跨工程合并；同一个路径在不同工程里可能是不同类型，谁先给出非空值用谁）。
	kindOfPath := make(map[string]string)
	for _, pid := range projectIDs {
		res, err := h.overviewAnalytics.Summary(ctx, &analyticsdto.SummaryReq{
			ProjectID: pid, From: rng.From, To: rng.To, PathLimit: overviewPathLimit,
			// 粒度必须跟趋势一致：小时粒度下订单侧给的是 YYYY-MM-DDTHH:00 的桶，
			// 浏览侧若仍按天回 YYYY-MM-DD，两者会被 buildTrend 归进不同的桶 ——
			// 图上多出一根来路不明的柱子，且两张图（销售额 / 浏览量）的横坐标对不上。
			Granularity: rng.Granularity,
		})
		if err != nil {
			snap.fail("analytics", pid, err)
			continue
		}
		snap.KPI.PageViews += res.Total
		// 按天浏览量并进同一根日期轴。analytics 的 Daily **只含有点击的天**，
		// 缺的天由 buildTrend 与订单侧的日期集求并集后统一处理（谁有数据谁说了算）。
		for _, d := range res.Daily {
			viewsByDay[d.Day] += d.Views
		}
		// 排行按路径合并**不需要** page 端口：它是 analytics 直接给的，
		// 端口未接线时少的只是「类型」那一列的标签。
		for _, p := range res.Paths {
			viewsByPath[p.Path] += p.Views
		}
		if h.overviewPageKinds == nil {
			continue
		}
		paths := make([]string, 0, len(res.Paths))
		for _, p := range res.Paths {
			paths = append(paths, p.Path)
		}
		kinds, err := h.overviewPageKinds.KindsOfPaths(ctx, pid, paths)
		if err != nil {
			snap.fail("page", pid, err)
			continue
		}
		for _, p := range res.Paths {
			if kinds[p.Path] == pageenums.PageKindArticle {
				snap.KPI.ArticleViews += p.Views
			}
			// 空值不覆盖已有值：多工程下同一个路径可能只有其中一处能查到类型。
			if kindOfPath[p.Path] == "" && kinds[p.Path] != "" {
				kindOfPath[p.Path] = kinds[p.Path]
			}
		}
	}
	snap.TopPages = buildTopPages(viewsByPath, kindOfPath)
}

// fail 记一次取数失败（去重模块名 + 结构化日志）。
func (s *overviewSnapshot) fail(block, projectID string, err error) {
	for _, b := range s.FailedBlocks {
		if b == block {
			return
		}
	}
	s.FailedBlocks = append(s.FailedBlocks, block)
	logger.Scene("workbench").With("project_id", projectID).With("block", block).
		Error(err, "概览页取数失败")
}

// 趋势图的画布参数（viewBox 宽度与模板保持一致）。
const (
	trendChartWidth = 900
	// trendBarGap 柱间隙：相邻两根贴在一起会被读成一根。
	trendBarGap = 4
	// trendBarMaxWidth 单根柱子的最大宽度（viewBox 单位，900 宽的画布上约 10%）。
	//
	// 柱宽原本只由点数决定：点数少时 step 会很大，1 个点时 step = 900 ——
	// 整张图只有一根贯穿全宽的横杠，「有数据」和「坐标轴画歪了」在屏幕上长得一样。
	trendBarMaxWidth = 88
	// trendLabelMax 最多显示几个月/日标签。
	//
	// 30 根柱子每个都带日期会重叠成一团黑 —— 稀疏到 12 个以内仍然能读出「这是哪一段」，
	// 而每根柱子的精确日期在 <title> 里（鼠标悬停可见）。
	trendLabelMax = 12
)

// buildTrend 把「桶键 → 点」的映射整理成升序序列，并算好每根柱子的位置与高度。
//
// granularity 决定怎么把上游的桶再合并一层：
//   - hour：上游（订单 / 浏览）已经是小时桶，原样用；
//   - day：原样用（订单与浏览的天然粒度）；
//   - week：按 ISO 周合并（Day 取那一周的周一）；
//   - month：按自然月合并（Day 取 YYYY-MM）。
//
// 合并放在这里而不是让上游换一种查询：上游回的是**事实**（每天多少单），
// 「怎么画」是展示层的取舍。按天画一年会得到 365 根不到 1px 的柱子 ——
// 读者什么也看不出来，只会以为图没加载。
//
// 柱宽由**点数**决定（早先是固定步长 70）：固定步长在 30 天的区间上会画到 viewBox
// 之外被裁掉，用户看到的是半张图而页面不会报任何错。
func buildTrend(byDay map[string]*overviewTrendPoint, viewsByDay map[string]int64, granularity string, symbol string) []overviewTrendPoint {
	buckets := make(map[string]*overviewTrendPoint, len(byDay)+len(viewsByDay))
	bucketKey := func(day string) string { return trendBucketKey(day, granularity) }
	// at 取（必要时新建）某一天的桶。**两侧都要走它**：订单与浏览量各自只覆盖
	// 一部分日期，谁先到都要能落进同一根柱子 —— 否则「有一半天只有浏览没有订单」时，
	// 那几天的浏览量会被静默丢掉（图短一截，且没有任何报错）。
	at := func(day string) *overviewTrendPoint {
		key := bucketKey(day)
		pt := buckets[key]
		if pt == nil {
			pt = &overviewTrendPoint{Day: key}
			buckets[key] = pt
		}
		return pt
	}
	for day, p := range byDay {
		pt := at(day)
		pt.Orders += p.Orders
		pt.NetSales += p.NetSales
	}
	for day, v := range viewsByDay {
		at(day).Views += v
	}
	points := make([]overviewTrendPoint, 0, len(buckets))
	for _, p := range buckets {
		points = append(points, *p)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Day < points[j].Day })

	var maxSales, maxViews int64
	for _, p := range points {
		if p.NetSales > maxSales {
			maxSales = p.NetSales
		}
		if p.Views > maxViews {
			maxViews = p.Views
		}
	}
	for i := range points {
		points[i].SalesLabel = moneyLabel(points[i].NetSales, symbol)
		points[i].DayLabel = trendLabel(points[i].Day, granularity)
		points[i].SalesHeightPct = barHeightPct(points[i].NetSales, maxSales)
		points[i].ViewsHeightPct = barHeightPct(points[i].Views, maxViews)
	}
	layoutTrendBars(points)
	return points
}

// trendBucketKey 把一个上游桶键折成当前粒度下的桶键（幂等：已经是该粒度的桶就原样回）。
//
// 上游的桶键有两种形态：按天 `YYYY-MM-DD`、按小时 `YYYY-MM-DDTHH:00`（见 utils.LayoutDay /
// LayoutHour）。这里只做**字符串截取**，不做时间解析 —— 解析一次要多一层错误分支，
// 而截取在两种形态上都是单调的（前缀相同时桶相同）。
func trendBucketKey(bucket, granularity string) string {
	switch granularity {
	case trendGranularityWeek:
		return weekStartOf(bucket[:min(10, len(bucket))])
	case trendGranularityMonth:
		if len(bucket) >= 7 {
			return bucket[:7]
		}
		return bucket
	default:
		return bucket
	}
}

// trendLabel 桶键 → 图上的人读标签。
//
// 按小时给 `10:00`（区间最多两天，日期由外层标题交代）、按月给 `2026-10`、
// 其余给 `10-05`。反过来的错法（按小时给完整日期时间）会让标签宽到重叠，
// 稀疏化只能减到几个，读者反而看不出时段。
func trendLabel(bucket, granularity string) string {
	switch granularity {
	case trendGranularityHour:
		if len(bucket) >= 16 {
			return bucket[11:16]
		}
	case trendGranularityMonth:
		return bucket
	default:
		if len(bucket) >= 10 {
			return bucket[5:10]
		}
	}
	return bucket
}

// barHeightPct 把某个值归一成 0~100 的柱高。
//
// 最小 4% 是「有量但很少」的那天不至于看不见 —— 归一到 0 会和「一单都没有」
// 长得一样，而这两件事对运营是不同的信息。
func barHeightPct(v, max int64) int {
	if max <= 0 {
		return 0
	}
	pct := int(v * 100 / max)
	if v > 0 && pct < 4 {
		pct = 4
	}
	return pct
}

// layoutTrendBars 按点数分配柱宽、横坐标与标签密度。
//
// 与 buildTrend 分开：那一步算的是**数据**（谁高谁矮），这一步算的是**版面**
// （谁在哪儿）。混在一起以后，改版面要读一整套聚合逻辑。
func layoutTrendBars(points []overviewTrendPoint) {
	n := len(points)
	if n == 0 {
		return
	}
	step := trendChartWidth / n
	if step < 1 {
		step = 1
	}
	barWidth := step - trendBarGap
	if barWidth < 2 {
		// 点极多时宁可让柱子贴在一起，也不要宽度为 0 的矩形（它不渲染，
		// 表现为「图里少了几天」，而那天其实有单）。
		barWidth = 2
	}
	// 上限：点数少时 step 会很大（今天 1 个点 → step=900），柱子被拉成一条贯穿
	// 全图的横杠，看起来既不像柱子也不像坐标轴。封顶之后柱子回到柱子的宽度，
	// 并靠下面的居中算式停在格子中间。
	if barWidth > trendBarMaxWidth {
		barWidth = trendBarMaxWidth
	}
	labelEvery := (n + trendLabelMax - 1) / trendLabelMax
	// 末尾那个点要不要补标签，取决于它离**上一个会打标签的点**还有多远。
	// 无条件补末尾（`|| i == n-1`）在区间长度不是 labelEvery 的整数倍时会造出一对挤在
	// 一起的标签 —— 实测「按小时 + 跨两天」共 49 个桶、labelEvery=5，末尾 20:00 与
	// 23:00 相隔 3 个桶（约 55px）而标签本身宽约 60px，直接叠在一起。
	// 判据取半个步长：够远就补（读者需要知道区间右端在哪），太近就不补（它会盖住前一个）。
	lastThinned := ((n - 1) / labelEvery) * labelEvery
	// 标签横坐标按对齐方式取柱子的左边缘 / 中心 / 右边缘。抽成闭包而不是让模板
	// 跟着 anchor 分支算：模板里改一处漏一处，会得到「首标签靠左对齐但坐标仍取中心」
	// 这种半对半错的位置，而它只是看着有点怪、不会报错。
	labelX := func(x, w int, anchor string) int {
		switch anchor {
		case trendAnchorStart:
			return x
		case trendAnchorEnd:
			return x + w
		default:
			return x + w/2
		}
	}
	for i := range points {
		points[i].X = i*step + (step-barWidth)/2
		points[i].BarWidth = barWidth
		points[i].ShowLabel = i%labelEvery == 0 ||
			(i == n-1 && i-lastThinned >= labelEvery/2)
		// 首尾靠边对齐，其余居中。全部居中的话，桶多时（按小时 24 个 → step≈20px）
		// 第一个标签的中心落在 x≈10，而 5 个字符的「00:00」要占 25px —— 左半边被
		// viewBox 裁掉，页面上看到的是「.7:00」这种缺半个字的标签。SVG 默认裁掉溢出
		// 内容，不报错、也不影响任何计数断言，只有看图才发现。
		//
		// 只有一个点时保持居中（那根柱子本来就在中间，不存在越界）。
		anchor := trendAnchorMiddle
		if n > 1 && i == 0 {
			anchor = trendAnchorStart
		} else if n > 1 && i == n-1 {
			anchor = trendAnchorEnd
		}
		points[i].LabelAnchor = anchor
		points[i].LabelX = labelX(points[i].X, barWidth, anchor)
	}
}

// 标签对齐方式（SVG text-anchor 的取值）。
const (
	trendAnchorStart  = "start"
	trendAnchorMiddle = "middle"
	trendAnchorEnd    = "end"
)

// weekStartOf 一天的 ISO 周一（YYYY-MM-DD）；解析失败原样返回（宁可少合并，不要丢点）。
func weekStartOf(day string) string {
	t, err := time.Parse(utils.LayoutDay, day)
	if err != nil {
		return day
	}
	return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7)).Format(utils.LayoutDay)
}

// buildTop 跨工程合并榜单：按销量降序（并列看金额与名字），重排名次后取前 N。
// buildTopPages 把「路径 → 浏览量」排成榜单（降序，取前 overviewPageTopLimit 条，重排名次）。
//
// 排序键要带路径收尾：浏览量并列时若只按那个数字排，同一次请求在两台机器上可能给出
// 不同顺序 —— 排行榜会随机抖动，测试也会偶发（与热销榜同一条考虑）。
func buildTopPages(viewsByPath map[string]int64, kindOfPath map[string]string) []overviewTopPage {
	items := make([]overviewTopPage, 0, len(viewsByPath))
	for path, views := range viewsByPath {
		kind := kindOfPath[path]
		items = append(items, overviewTopPage{
			Path: path, Views: views, Kind: kind, KindKey: overviewPageKindKey(kind),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Views != items[j].Views {
			return items[i].Views > items[j].Views
		}
		return items[i].Path < items[j].Path
	})
	if len(items) > overviewPageTopLimit {
		items = items[:overviewPageTopLimit]
	}
	for i := range items {
		items[i].Rank = i + 1
	}
	return items
}

// overviewPageKindKey 页面类型 → i18n 词条 key（认不出的类型回空串，不渲染标签）。
//
// 键名与 pageenums 的常量逐条对齐；这里用 switch 而不是拼字符串，
// 是为了让「page 模块新增一种类型」在编译期就能被看见（拼串会静默给出一个不存在的 key，
// 页面上表现为那枚标签凭空消失）。
func overviewPageKindKey(kind string) string {
	switch kind {
	case pageenums.PageKindHome:
		return "admin.dashboard.pageKind.home"
	case pageenums.PageKindPage:
		return "admin.dashboard.pageKind.page"
	case pageenums.PageKindArticle:
		return "admin.dashboard.pageKind.article"
	case pageenums.PageKindTag:
		return "admin.dashboard.pageKind.tag"
	case pageenums.PageKindArchive:
		return "admin.dashboard.pageKind.archive"
	case pageenums.PageKindSearch:
		return "admin.dashboard.pageKind.search"
	// 404 也进路径排行，而且它往往是运营最该看见的一类流量（用户在找不存在的页）。
	case pageenums.PageKindNotFound:
		return "admin.dashboard.pageKind.notFound"
	default:
		return ""
	}
}

func buildTop(products []overviewTopProduct) []overviewTopProduct {
	if len(products) == 0 {
		return nil
	}
	sort.SliceStable(products, func(i, j int) bool {
		if products[i].Quantity != products[j].Quantity {
			return products[i].Quantity > products[j].Quantity
		}
		if products[i].ProductName != products[j].ProductName {
			return products[i].ProductName < products[j].ProductName
		}
		return products[i].SKU < products[j].SKU
	})
	if len(products) > overviewTopLimit {
		products = products[:overviewTopLimit]
	}
	for i := range products {
		products[i].Rank = i + 1
	}
	return products
}

// moneyLabel 分 → 展示串（货币符号 + 两位小数 + 千分位）。
//
// symbol 由调用方从后台字典取（见 currencySymbol），本函数只负责拼。
//
// 与 order/cart 的两处换算不是同一件事：那边一个给「X 元」拼句用（裸数字）、
// 一个给购物车行用（¥ 前缀无千分位），这里是概览 KPI 的展示形态。
// 跨工程求和必须在本层重算 —— 上游给的是**单个工程**的 label，直接相加是错的。
func moneyLabel(cents int64, symbol string) string {
	raw := formatCents(cents)
	sym := strings.TrimSpace(symbol)
	if sym == "" {
		return raw
	}
	// 符号紧贴数字（¥300.50），不留空格 —— 符号与代码不是一类东西：
	// 「CNY 300.50」是把口径标识当符号用，人读的是符号，数字紧跟符号才是金额的样子。
	return sym + raw
}

// formatCents 分 → 千分位 + 两位小数（纯数字，不含货币）。
func formatCents(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	intPart := cents / 100
	frac := cents % 100
	digits := strconv.FormatInt(intPart, 10)
	var b strings.Builder
	for i, ch := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	out := fmt.Sprintf("%s.%02d", b.String(), frac)
	if neg {
		return "-" + out
	}
	return out
}

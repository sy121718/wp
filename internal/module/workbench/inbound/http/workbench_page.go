package workbenchhttp

// 为什么组装点在 workbench：概览页是唯一需要同时看这几个模块的地方，而模块之间不能
// 互相依赖（表隔离：analytics 读不到 orders，order 读不到 pages）。workbench 是它们
// 共同的消费者，所以由它把各自的只读结论拼起来 —— 各模块只回答自己领域的问题，
// 不参与对方的计算。
//
// 时间口径：**UTC 日界**，与 order 的按天桶、analytics 的按天聚合逐字一致
//（analytics: `(viewed_at AT TIME ZONE 'UTC')::date`）。用本地日界会让「今日 KPI」
// 与柱图最后一根柱子算出两个数，而两者都会被当成对的。

// 为什么区间解析在**这里**算完再往下传：三个块（KPI / 趋势 / 榜单）必须是同一个窗口。
// 让每个端口各自解释「本月」，迟早出现「KPI 说本月 12 单、趋势图却画的是近 7 天」——
// 页面在自相矛盾，而每一块单独看都对。所以这里只算出 from/to 两个日期字符串往下传，
// 下游不认识「本月」这个词。
//
// 日界统一 UTC，与 order 的按天桶、analytics 的按天聚合同口径（见 dashboard_overview.go 文件头）。

// 背景：workbench.js 的 renderTree 用 100+ 行 DOM 代码递归建树并给每个节点绑 6 类事件。
// 本文件把「树 HTML」搬到服务端（Jet 片段），客户端只保留一次事件委托
//（选中/拖拽/右键/重命名/caret 折叠），DOM 由服务端产出。
//
// 端点：POST /workbench/outline，参数 document（草稿 JSON）+ selectedId + filter。

// 画布编辑的是该实例自己的覆盖文档（迁移 281），保存走 presentation.SaveOverrideDocument，
// 不影响共享模板。

// 从 dashboard_handle.go 拆出：单一职责（编辑器桥接），与页面渲染/预览解耦。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder"
	"go_wp/internal/module/analytics/dto"
	"go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/module/contenttemplate/dto"
	"go_wp/internal/module/order/dto"
	"go_wp/internal/module/page/enums"
	"go_wp/internal/module/plugin/contract"
	"go_wp/internal/module/presentation/contract"
	"go_wp/internal/module/presentation/dto"
	"go_wp/internal/module/presentation/enums"
	"go_wp/internal/module/sysconfig/dto"
	"go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/internal/templates"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
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

// dashboardPath 概览页自己的地址。
//
// 它是 `/admin` 而不是 `/admin/dashboard` —— 仪表盘挂在 workbench 的**根级前缀组**里，
// 注册语句是 router.go 的 SetupWorkbenchRoutes 中的 `g.GET("/admin", h.Dashboard)`。
// 写 `/admin/dashboard` 得到的是 404（页面不报错，只是筛选条点下去什么都不发生），
// 所以模板里的 form action 与本常量必须同源，并由
// TestAdminDashboardFormActionMatchesRoute 钉住。
const dashboardPath = "/admin"

// 预设键（页面上那一排按钮）。
const (
	rangeToday     = "today"
	rangeYesterday = "yesterday"
	rangeWeek      = "week"
	rangeMonth     = "month"
	rangeYear      = "year"
	rangeCustom    = "custom"
)

// rangeDefault 默认区间：本周。
//
// 为什么不是「今日」：概览页最常见的问法是「最近怎么样」，而今天的数字在上午
// 往往只有几单，看不出形状。本周既有几天的量、又不至于把「今天」淹掉。
const rangeDefault = rangeWeek

// rangeMaxDays 自定义区间的天数上限（含首尾）。
//
// 挡的是 `from=1970-01-01` 这类请求：它会变成一次全表扫描，而概览页是登录后的第一跳。
// 366 天足够覆盖「去年同期」这类正常需求。
const rangeMaxDays = 366

// trendDailyMaxDays 超过这个天数，趋势就按**周**聚合。
//
// 按天画 365 根柱子在页面上既看不清也没意义（每根不足 1px），按周聚合后最多 53 根，
// 形状仍然读得出来。
const trendDailyMaxDays = 31

// trendHourlyMaxDays 不超过这个天数时，默认按**小时**聚合。
//
// 这是「今天/昨天」区间唯一的正确粒度：按天看一天只有一根柱子 —— 形状、峰值、
// 时段分布全都读不出来，而用户打开「今日」想看的正是这些。
const trendHourlyMaxDays = 2

// 趋势粒度（也是 URL 参数 granularity 的白名单）。
const (
	trendGranularityHour  = "hour"
	trendGranularityDay   = "day"
	trendGranularityWeek  = "week"
	trendGranularityMonth = "month"
)

// granularityLabelKeys 粒度按钮的词条键（顺序即页面上的顺序）。
var trendGranularityKeys = []string{
	trendGranularityHour,
	trendGranularityDay,
	trendGranularityWeek,
	trendGranularityMonth,
}

// granularityLabelKey 粒度 → 词条键。
func granularityLabelKey(g string) string {
	return "admin.dashboard.trend.granularity." + g
}

// defaultGranularity 区间长度对应的默认粒度。
//
// 默认跟着区间走（短区间按小时、长区间按周），但用户可以显式覆盖 —— 早先这里刻意
// 不给下拉，理由是「那等于让人自己算一遍多长的区间该用什么粒度」；实测下来这个理由
// 只对「默认值」成立：真正想按小时看一整周、或按天看一年的人，没有入口就只能换区间，
// 而他想要的恰恰是「这个区间 + 那个粒度」。所以默认值仍按区间算，但可覆盖。
func defaultGranularity(days int) string {
	switch {
	case days <= trendHourlyMaxDays:
		return trendGranularityHour
	case days > trendDailyMaxDays:
		return trendGranularityWeek
	default:
		return trendGranularityDay
	}
}

// overviewRange 一个已生效的区间（含首尾两端）。
type overviewRange struct {
	// Key 生效的预设键（自定义区间被收敛后会变成 custom）。
	Key string
	// From / To YYYY-MM-DD，**含**首尾两端。
	From string
	To   string
	// Days 区间天数（含首尾）：1 表示就是一天。
	Days int
	// Granularity 趋势的聚合粒度（hour / day / week / month）。
	//
	// 默认由区间长度算出（见 defaultGranularity），URL 的 granularity 参数可覆盖。
	Granularity string
	// GranularityPresets 粒度切到其它档时的按钮（保留当前区间与其它筛选）。
	GranularityPresets []granularityPreset
	// GranularityTitleKey / GranularityTitle 趋势卡标题（随粒度变：按小时 / 按天 / 按周 / 按月）。
	//
	// 在服务端算好而不是在模板里 if/else 四档：Jet 没有 switch，四个分支写在模板里
	// 会让「有哪些粒度」这件事同时存在于模板与白名单两处，加一档就得记得改两边。
	GranularityTitleKey string
	GranularityTitle    string
	// Clamped 为真表示请求的区间被收敛过（超长 / 未来日期 / 反向）。
	//
	// 页面据此提示一句「已按最长 366 天截取」：静默改口径比报错更糟 ——
	// 用户会以为自己看到的就是他选的那一段。
	Clamped bool
}

// rangePreset 筛选条上的一个预设按钮。
type rangePreset struct {
	Key string
	// LabelKey 完整词条键（模板直接用它取词条，不做字符串拼接 ——
	// Jet 里的拼接要依赖表达式的求值顺序，而这里没有非拼不可的理由）。
	LabelKey string
	Label    string
	// URL 直接可点（带上当前工程等其它筛选），模板不必拼 query。
	URL    string
	Active bool
}

// parseOverviewRange 解析请求里的区间参数，收敛到合法值。
//
// 收敛而不是拒绝：概览页是「打开就看」的页面，为一个越界的日期参数回 400
// 只会让用户看到一页错误 —— 而他要的信息（最近的情况）本来就能给。
func parseOverviewRange(c *gin.Context, today time.Time) overviewRange {
	key := strings.TrimSpace(c.Query("range"))
	if key == "" {
		key = rangeDefault
	}
	day := func(t time.Time) string { return t.Format(utils.LayoutDay) }

	var r overviewRange
	switch key {
	case rangeToday:
		d := day(today)
		r = newRange(rangeToday, d, d, false)
	case rangeYesterday:
		d := day(today.AddDate(0, 0, -1))
		r = newRange(rangeYesterday, d, d, false)
	case rangeWeek:
		r = defaultRange(today)
	case rangeMonth:
		first := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
		r = newRange(rangeMonth, day(first), day(today), false)
	case rangeYear:
		first := time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		r = newRange(rangeYear, day(first), day(today), false)
	case rangeCustom:
		r = parseCustomRange(c, today)
	default:
		// 认不出的键按默认走（老链接、手改的 URL）：不报错、也不猜语义。
		r = defaultRange(today)
	}
	return applyGranularity(r, c.Query("granularity"))
}

// granularityTitleKey 粒度 → 趋势卡标题词条键。
func granularityTitleKey(g string) string {
	return "admin.dashboard.trend.title." + g
}

// granularityTitle 粒度 → 标题中文兜底。
func granularityTitle(g string) string {
	switch g {
	case trendGranularityHour:
		return "趋势（按小时）"
	case trendGranularityWeek:
		return "趋势（按周）"
	case trendGranularityMonth:
		return "趋势（按月）"
	default:
		return "趋势（按天）"
	}
}

// applyGranularity 用 URL 参数覆盖默认粒度（不合法就保持默认）。
//
// 「按小时」超出区间上限时**回落到默认粒度**而不是把请求发下去：service 会回参数错误，
// 而页面把它渲染成「趋势取数失败」—— 用户看到的是一块错误，而他能接受的结果
// （这段区间按天看）本来就在手边。这与「区间收敛」是同一条原则。
func applyGranularity(r overviewRange, raw string) overviewRange {
	if g := strings.TrimSpace(raw); isTrendGranularity(g) {
		if g != trendGranularityHour || r.Days <= trendHourlyMaxDays {
			r.Granularity = g
		}
	}
	r.GranularityTitleKey = granularityTitleKey(r.Granularity)
	r.GranularityTitle = granularityTitle(r.Granularity)
	return r
}

// isTrendGranularity 粒度白名单（未知值一律忽略，与 range 的未知键同一条处理）。
func isTrendGranularity(g string) bool {
	for _, k := range trendGranularityKeys {
		if k == g {
			return true
		}
	}
	return false
}

// defaultRange 默认区间（本周）。
//
// ISO 周：周一为起点（time.Weekday 的周日是 0，要单独折一下）。
func defaultRange(today time.Time) overviewRange {
	offset := (int(today.Weekday()) + 6) % 7
	d := func(t time.Time) string { return t.Format(utils.LayoutDay) }
	return newRange(rangeDefault, d(today.AddDate(0, 0, -offset)), d(today), false)
}

// parseCustomRange 解析 from / to。
//
// 四种收敛各自的理由：
//   - 解析不出来 → 用默认区间（可能是手改的 URL，或复制时截断了）；
//   - from > to → 交换。用户想表达的是「这几天」，顺序写反不该变成空区间；
//   - to 在今天之后 → 收到今天。未来的订单不存在，窗口伸到未来只会让「日均」变小；
//   - 超过 rangeMaxDays → 保留 **to** 往前截，保住「最近」这一半（用户真正关心的那半）。
func parseCustomRange(c *gin.Context, today time.Time) overviewRange {
	from, errFrom := time.Parse(utils.LayoutDay, strings.TrimSpace(c.Query("from")))
	to, errTo := time.Parse(utils.LayoutDay, strings.TrimSpace(c.Query("to")))
	if errFrom != nil || errTo != nil {
		return defaultRange(today)
	}
	from, to = from.UTC(), to.UTC()
	clamped := false
	if from.After(to) {
		from, to = to, from
		clamped = true
	}
	if to.After(today) {
		to = today
		clamped = true
	}
	if days := int(to.Sub(from).Hours()/24) + 1; days > rangeMaxDays {
		from = to.AddDate(0, 0, -(rangeMaxDays - 1))
		clamped = true
	}
	r := newRange(rangeCustom, from.Format(utils.LayoutDay), to.Format(utils.LayoutDay), clamped)
	return r
}

// newRange 组装区间并算出天数与趋势粒度。
func newRange(key, from, to string, clamped bool) overviewRange {
	f, errF := time.Parse(utils.LayoutDay, from)
	t, errT := time.Parse(utils.LayoutDay, to)
	days := 1
	if errF == nil && errT == nil {
		days = int(t.Sub(f).Hours()/24) + 1
		if days < 1 {
			days = 1
		}
	}
	return overviewRange{
		Key:         key,
		From:        from,
		To:          to,
		Days:        days,
		Granularity: defaultGranularity(days),
		Clamped:     clamped,
	}
}

// granularityPreset 粒度切换条上的一个按钮。
type granularityPreset struct {
	Key      string
	LabelKey string
	Label    string
	URL      string
	Active   bool
}

// granularityPresetURL 拼一个粒度按钮的地址：保留区间（含自定义的 from/to）与工程。
//
// 自定义区间必须把 from/to 一起带走 —— 否则「自定义 + 按小时」点一下就掉回默认区间，
// 而用户只是想换个粒度看同一段数据。
func granularityPresetURL(c *gin.Context, g string, active overviewRange) string {
	q := url.Values{}
	if p := strings.TrimSpace(c.Query("project")); p != "" {
		q.Set("project", p)
	}
	q.Set("range", active.Key)
	if active.Key == rangeCustom {
		q.Set("from", active.From)
		q.Set("to", active.To)
	}
	q.Set("granularity", g)
	return dashboardPath + "?" + q.Encode()
}

// granularityPresets 粒度按钮（顺序 = trendGranularityKeys）。
func granularityPresets(c *gin.Context, active overviewRange) []granularityPreset {
	out := make([]granularityPreset, 0, len(trendGranularityKeys))
	for _, g := range trendGranularityKeys {
		out = append(out, granularityPreset{
			Key:      g,
			LabelKey: granularityLabelKey(g),
			Label:    granularityLabel(g),
			URL:      granularityPresetURL(c, g, active),
			Active:   active.Granularity == g,
		})
	}
	return out
}

// granularityLabel 粒度按钮的中文兜底文案。
func granularityLabel(g string) string {
	switch g {
	case trendGranularityHour:
		return "小时"
	case trendGranularityDay:
		return "天"
	case trendGranularityWeek:
		return "周"
	case trendGranularityMonth:
		return "月"
	default:
		return g
	}
}

// rangePresetURL 拼一个预设按钮的地址：保留其它筛选，只换 range（自定义时清掉 from/to）。
//
// 逐参数白名单而不是把原 query 整串拼回去：那样会把 from/to 一起带到预设链接里，
// 于是「点本周」带着上次的自定义区间一起提交，页面回到 custom。
//
// **刻意不保留 granularity**：换区间时粒度回到该区间的默认值。保留会让「小时 + 点本月」
// 得到一个 720 桶的请求（service 直接拒），而用户的本意是「看本月」。粒度按钮自己
// 则保留区间（见 granularityPresetURL），两个方向的需求不对称，链接的拼法也不同。
func rangePresetURL(c *gin.Context, key, from, to string) string {
	q := url.Values{}
	if p := strings.TrimSpace(c.Query("project")); p != "" {
		q.Set("project", p)
	}
	q.Set("range", key)
	if key == rangeCustom {
		if from != "" {
			q.Set("from", from)
		}
		if to != "" {
			q.Set("to", to)
		}
	}
	return dashboardPath + "?" + q.Encode()
}

// rangePresets 筛选条上的一排按钮。
//
// Label 是**中文兜底**：页面侧用 i18n key 取词条，取不到就显示这个。
// 键名规则见 check-i18n-coverage：admin.<模块>.<语义>。
func rangePresets(c *gin.Context, active overviewRange) []rangePreset {
	keys := []string{rangeToday, rangeYesterday, rangeWeek, rangeMonth, rangeYear, rangeCustom}
	out := make([]rangePreset, 0, len(keys))
	for _, k := range keys {
		out = append(out, rangePreset{
			Key:      k,
			LabelKey: "admin.dashboard.range." + k,
			Label:    rangePresetLabel(k),
			URL:      rangePresetURL(c, k, active.From, active.To),
			Active:   active.Key == k,
		})
	}
	return out
}

// rangePresetLabel 预设的中文兜底文案（词条在迁移 550）。
func rangePresetLabel(key string) string {
	switch key {
	case rangeToday:
		return "今日"
	case rangeYesterday:
		return "昨日"
	case rangeWeek:
		return "本周"
	case rangeMonth:
		return "本月"
	case rangeYear:
		return "本年"
	case rangeCustom:
		return "自定义"
	default:
		return key
	}
}

// withPresets 补上筛选条的预设按钮（URL 已按当前筛选拼好）。
//
// 单独一步而不是塞进 collectOverview：那一层算的是数据，不认识也不该认识 *gin.Context
// —— 筛选条的链接是展示层的事。漏调它的代价只是「按钮没有选中态」，
// 而不是「数据算错」，两件事不该绑在一次调用里。
func withPresets(c *gin.Context, snap overviewSnapshot) overviewSnapshot {
	snap.Presets = rangePresets(c, snap.Range)
	snap.GranularityPresets = granularityPresets(c, snap.Range)
	return snap
}

// InspectorPanel 渲染选中节点的检查器面板片段。
func (h *Handle) InspectorPanel(c *gin.Context) {
	nodeID := strings.TrimSpace(c.PostForm("nodeId"))
	node, err := workbenchservice.FindDocNode(json.RawMessage(c.PostForm("document")), nodeID)
	if err != nil || node == nil {
		c.HTML(http.StatusOK, "fragments/inspector_panel", gin.H{
			"NodeID": "",
			// 空态分支也要给 t：片段模板里的取词（workbench.ui.inspector.empty 等）缺 t 时
			// Jet 会静默输出空串（不是报错），空面板会变成一句话都没有。
			"t": templates.TranslateFunc(response.RequestLanguage(c)),
		})
		return
	}
	// tab：content / style（空 = 渲染全部，向后兼容旧调用）。
	tab := strings.TrimSpace(c.PostForm("tab"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	// 装配链（组件 schema 取数 → 解包 → 字段构造 → 分组桶 → 重复项面板）全在
	// service.InspectorSections：任一步取数失败都以 error 出来，本函数只决定出口形态
	//（保持 text/plain 不变 —— 前端是 fetch → r.text() → morphHTML，不检查 r.ok，
	// 换成 JSON 反而会把一段 JSON 铺进面板）。
	sections, err := h.svc.InspectorSections(c.Request.Context(), node, tab, projectID, workbenchTrFunc(c))
	if err != nil {
		c.String(http.StatusInternalServerError, shell.PageInternalText(c))
		return
	}
	c.HTML(http.StatusOK, "fragments/inspector_panel", gin.H{
		"NodeID": nodeID, "NodeType": node.Type,
		"Sections": sections,
		// 片段模板的取词函数（与后台页面同一份 TranslateFunc）：片段不经 shell.Prepare，
		// 不注入的话新增文案只能写死在模板里，英文界面上会留下中文。
		"t": templates.TranslateFunc(response.RequestLanguage(c)),
	})
}

// OutlineTree 渲染结构树片段（过滤规则与前端一致：节点或任一后代命中即保留整条链路）。
func (h *Handle) OutlineTree(c *gin.Context) {
	var page struct {
		Root []workbenchservice.OutlineNode `json:"root"`
	}
	if doc := c.PostForm("document"); doc != "" {
		_ = json.Unmarshal([]byte(doc), &page)
	}
	selectedID := strings.TrimSpace(c.PostForm("selectedId"))
	filter := strings.ToLower(strings.TrimSpace(c.PostForm("filter")))
	c.HTML(http.StatusOK, "fragments/outline_tree", gin.H{
		"HTML": workbenchservice.RenderOutlineHTML(page.Root, selectedID, filter, workbenchTrFunc(c)),
	})
}

// workbench_handle.go - 工作台页面与画布（编辑器壳 / 块与模板画布 / 模板预览）。

// Workbench 可视化编辑器外壳：注入 Page 草稿 AST 与保存接口所需元数据。
// ?block=ID 进入全局块编辑模式（同一画布，保存走块接口、无发布链）。
// ?template=ID 进入内容模板编辑模式（保存走 contenttemplate.Update，预览需样例实体）。
func (h *Handle) Workbench(c *gin.Context) {
	if blockID := strings.TrimSpace(c.Query("block")); blockID != "" {
		h.workbenchBlock(c, blockID)
		return
	}
	if templateID := strings.TrimSpace(c.Query("template")); templateID != "" {
		h.workbenchTemplate(c, templateID)
		return
	}
	if instanceID := strings.TrimSpace(c.Query("instance")); instanceID != "" {
		h.workbenchInstance(c, instanceID)
		return
	}
	pageID := strings.TrimSpace(c.Query("id"))
	if pageID == "" {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrMissingPageID))
		return
	}
	// 走统一出口 pageOf：Detail 的 projectID 是必填的越权防护 scope，只传 ID 会被
	// 契约层判为「参数缺失」，而这里把它显示成「页面不存在」—— 一个真实的 404 与
	// 一个漏传 scope 的调用，在页面上长得一模一样。
	page, err := h.svc.PageByID(c.Request.Context(), pageID)
	if err != nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrPageNotFound))
		return
	}
	documentJSON, err := json.Marshal(page.DraftDocument)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrDraftEncodeFailed))
		return
	}
	// 启用插件的组件库摘要与区块预设（palette 注入，docs/06 §5/§5.2）。
	var pluginComponents []plugincontract.ComponentSummary
	var pluginPresets []plugincontract.PresetSummary
	asm := h.svc.PluginAssembly(c.Request.Context())
	if asm != nil {
		pluginComponents = asm.Components
		pluginPresets = asm.Presets
	}
	// 预设空时给空数组而非 nil（前端按数组读取，避免 undefined）。
	if pluginPresets == nil {
		pluginPresets = []plugincontract.PresetSummary{}
	}
	metaJSON, err := json.Marshal(gin.H{
		// target 编辑目标描述符（EDT-017）：前端按它决定保存端点与请求体键名，
		// 不认识任何一种目标类型 —— 新增目标时前端零改动。
		"target":    workbenchTargetOf(EditTargetPage),
		"pageId":    page.ID,
		"projectId": page.ProjectID,
		"draftPath": page.DraftPath,
		"version":   page.DraftVersion,
		// 全局块引用（core.globalref）候选：本工程全部块（组件库「全局块」分组）。
		"blocks": h.svc.BlockSummaries(c.Request.Context(), page.ProjectID),
		// 全局设置面板：页面挂接的主题与当前设置（颜色/字体），可就地修改保存。
		"themeId":       workbenchservice.ThemeIDOf(page),
		"themeSettings": h.svc.ThemeSettingsOf(c.Request.Context(), page),
		// 启用插件组件（组件库「插件组件」分组，type/label/hint/初始 props）。
		"plugins": pluginComponents,
		// 启用插件区块预设（组件库「区块预设」分组，id/label/category/thumbnail/document）。
		"presets": pluginPresets,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrEditorMetaEncodeFailed))
		return
	}
	// 组件 Inspector 面板 schema（docs/02-C3）：声明式 Controls 驱动检查器表单，
	// 前端按 content/style/advanced 分组渲染，替代硬编码字段。
	// 插件组件 schema 合并（与内置同构，docs/06 §5：上传即出现在检查器）。
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaBuildFailed))
		return
	}
	if asm != nil {
		for t, data := range asm.InspectorSchemas {
			schemas[t] = data
		}
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaEncodeFailed))
		return
	}
	c.HTML(http.StatusOK, "workbench/layout", shell.Prepare(c, gin.H{
		"title":     workbenchservice.WorkbenchTitle(page, workbenchTrFunc(c)),
		"pageId":    page.ID,
		"isBlock":   false,
		"draftPath": page.DraftPath,
		"version":   page.DraftVersion,
		"document":  shell.JsonSafe(string(documentJSON)),
		"meta":      shell.JsonSafe(string(metaJSON)),
		"schemas":   shell.JsonSafe(string(schemasJSON)),
		"jsVer":     workbenchservice.StaticJSVersion(),
	}))
}

// workbenchBlock 全局块编辑模式：复用工作台画布与检查器，
// 保存走 /api/block/update（无发布链、无 URL），meta.saveBase 指示前端切换接口前缀。
func (h *Handle) workbenchBlock(c *gin.Context, blockID string) {
	block, err := h.svc.BlockByID(c.Request.Context(), blockID)
	if err != nil || block == nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrBlockNotFound))
		return
	}
	documentJSON, err := json.Marshal(block.Document)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrBlockEncodeFailed))
		return
	}
	meta := gin.H{
		// target 编辑目标描述符（EDT-017）。
		"target":    workbenchTargetOf(EditTargetBlock),
		"pageId":    block.ID, // 复用键名：前端保存逻辑按 saveBase 切换接口
		"projectId": block.ProjectID,
		"saveBase":  "block",
		"blockName": block.Name,
		"kind":      block.Kind,
		"draftPath": "",
		"version":   0,
	}
	// returnUrl：菜单页「新建面板块并编辑」一路带过来的回跳目标。
	//
	// 白名单收敛在这一处（shell.LocalReturnPath）：不是站内相对路径的值直接**丢弃** ——
	// meta 里没有这个键，保存后留在工作台，与普通块编辑完全一样。这里不做兜底跳转，
	// 因为「没带 returnUrl」与「带了非法 returnUrl」对用户是同一件事：不该离开编辑器。
	// 消费侧（SaveBlockContent）会再校验一次：meta 只是搬运，不是信任边界。
	if back := shell.LocalReturnPath(c.Query("returnUrl")); back != "" {
		meta["returnUrl"] = back
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrEditorMetaEncodeFailed))
		return
	}
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaBuildFailed))
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaEncodeFailed))
		return
	}
	c.HTML(http.StatusOK, "workbench/layout", shell.Prepare(c, gin.H{
		"title":    workbenchShortText(c, workbenchenums.TitleBlockPrefix) + block.Name,
		"pageId":   block.ID,
		"isBlock":  true,
		"document": shell.JsonSafe(string(documentJSON)),
		"meta":     shell.JsonSafe(string(metaJSON)),
		"schemas":  shell.JsonSafe(string(schemasJSON)),
		"jsVer":    workbenchservice.StaticJSVersion(),
	}))
}

// workbenchTemplate 内容模板编辑模式（EDT-001）：复用工作台画布与检查器，
// 保存走 /api/contenttemplate/update。
//
// 两种预览模式（判据是**模板自己的 entity_type**，不是请求参数）：
//   - 结构模板（页眉 / 页脚）→ **无样例实体模式**：它们不是内容实体、也不接受字段
//     绑定（服务端 validateDocumentMode 对结构类型直接拒绝绑定），所以预览不需要
//     样例实体，entityId 传了也一律忽略；
//   - 内容实体模板（product / article / …）→ 仍**必须**有样例实体：字段绑定要按一条
//     真实记录解析，缺它只能看到空白组件（这正是这条校验存在的理由）。
func (h *Handle) workbenchTemplate(c *gin.Context, templateID string) {
	if !h.svc.ContentTemplatesReady() {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrContentTemplateEditNotAssembled))
		return
	}
	// projectID 留空：这里只按 id 取模板（工程作用域由预览入口 previewTemplateTarget 负责）。
	tpl, err := h.svc.TemplateByID(c.Request.Context(), templateID, "")
	if err != nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrTemplateNotFound))
		return
	}
	// 无实体模式按**模板行**判定：查询参数是请求方给的，拿它判等于让
	// 「product 模板 + entityType=header」把无实体模式开给普通模板（样例实体校验被绕过）。
	noEntity := contenttemplatecontract.IsStructureTemplateType(tpl.EntityType)
	entityID := strings.TrimSpace(c.Query("entityId"))
	entityType := strings.TrimSpace(c.Query("entityType"))
	if entityType == "" || noEntity {
		// 结构模板的类型以模板行为准：伪造的 entityType 不能把预览引到实体解析那条路。
		entityType = tpl.EntityType
	}
	if noEntity {
		// 结构模板没有实体来源：请求带来的 entityId 一律丢弃（无实体模式不解析字段绑定）。
		entityID = ""
	} else if entityID == "" {
		// 占位符 {type} 由词条携带：译文顺序可能与中文不同，所以不在这里拼进句子。
		c.String(http.StatusBadRequest,
			strings.ReplaceAll(workbenchShortText(c, workbenchenums.ErrPreviewEntityIDRequired), "{type}", entityType))
		return
	}
	// 装配校验按模式分流：无实体模式走 page 编译管线，用不到模板预览端口。
	if !noEntity && h.templatePreview == nil {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrContentTemplateEditNotAssembled))
		return
	}
	if noEntity && h.pages == nil {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrStructureTemplatePreviewNotAssembled))
		return
	}
	projectID := strings.TrimSpace(c.Query("projectId"))
	documentJSON, err := json.Marshal(tpl.DraftDocument)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrTemplateEncodeFailed))
		return
	}
	metaJSON, err := json.Marshal(gin.H{
		// target 编辑目标描述符（EDT-017）。
		"target":       workbenchTargetOf(EditTargetTemplate),
		"pageId":       tpl.ID,
		"saveBase":     "template",
		"templateName": tpl.Name,
		"entityType":   entityType,
		"entityId":     entityID,
		"noEntity":     noEntity,
		"projectId":    projectID,
		"draftPath":    "",
		"version":      tpl.DraftVersion,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrEditorMetaEncodeFailed))
		return
	}
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaBuildFailed))
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaEncodeFailed))
		return
	}
	previewQS := workbenchservice.TemplatePreviewQuery(tpl.ID, entityType, entityID, projectID)
	c.HTML(http.StatusOK, "workbench/layout", shell.Prepare(c, gin.H{
		"title":      workbenchShortText(c, workbenchenums.TitleTemplatePrefix) + tpl.Name,
		"pageId":     tpl.ID,
		"isBlock":    false,
		"isTemplate": true,
		// 画布顶栏的模式提示：结构模板的预览不解析字段绑定，这件事要在编辑器里看得见
		//（否则作者会以为「页眉里该出现的商品名没出现」是渲染坏了）。
		"isStructureTemplate": noEntity,
		"document":            shell.JsonSafe(string(documentJSON)),
		"meta":                shell.JsonSafe(string(metaJSON)),
		"schemas":             shell.JsonSafe(string(schemasJSON)),
		"previewQS":           previewQS,
		"jsVer":               workbenchservice.StaticJSVersion(),
	}))
}

// previewTemplateTarget 取预览目标模板；模板不存在时已写响应并返回 ok=false。
func (h *Handle) previewTemplateTarget(c *gin.Context, templateID, projectID string) (tpl *contenttemplatedto.TemplateResp, ok bool) {
	if !h.svc.ContentTemplatesReady() {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrContentTemplateEditNotAssembled))
		return nil, false
	}
	tpl, err := h.svc.TemplateByID(c.Request.Context(), templateID, projectID)
	if err != nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrTemplateNotFound))
		return nil, false
	}
	return tpl, true
}

// TemplatePreview 基于已保存模板 + 样例实体编译预览（画布 iframe GET）。
//
// 结构模板（页眉 / 页脚）没有样例实体：entityId 缺省/为空时按无实体模式渲染
// （判据是**模板自己的 entity_type**，见 workbenchTemplate 的同名说明）。
func (h *Handle) TemplatePreview(c *gin.Context) {
	templateID := strings.TrimSpace(c.Query("template"))
	if templateID == "" {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrTemplateParamRequired))
		return
	}
	projectID := c.Query("projectId")
	tpl, ok := h.previewTemplateTarget(c, templateID, projectID)
	if !ok {
		return
	}
	if contenttemplatecontract.IsStructureTemplateType(tpl.EntityType) {
		// 无实体模式：结构模板的 entityId 一律忽略（它不是内容实体，没有字段来源）。
		h.renderStructureTemplatePreview(c, tpl.DraftDocument, projectID, c.Query("editor") == "1")
		return
	}
	if h.templatePreview == nil {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrContentTemplatePreviewNotAssembled))
		return
	}
	entityType := strings.TrimSpace(c.Query("entityType"))
	entityID := strings.TrimSpace(c.Query("entityId"))
	if entityType == "" || entityID == "" {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrPreviewParamsRequired))
		return
	}
	h.renderTemplatePreview(c, templateID, entityType, entityID, projectID, nil,
		c.Query("editor") == "1")
}

// TemplatePreviewDraft 基于未保存 AST + 样例实体返回临时预览（POST，画布刷新）。
// 结构模板同 TemplatePreview：走无实体模式，草稿文档直接编译。
func (h *Handle) TemplatePreviewDraft(c *gin.Context) {
	templateID := strings.TrimSpace(c.PostForm("id"))
	document := json.RawMessage(c.PostForm("draftDocument"))
	if templateID == "" || len(document) == 0 {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrPreviewParamsIncomplete))
		return
	}
	projectID := c.PostForm("projectId")
	tpl, ok := h.previewTemplateTarget(c, templateID, projectID)
	if !ok {
		return
	}
	if contenttemplatecontract.IsStructureTemplateType(tpl.EntityType) {
		h.renderStructureTemplatePreview(c, document, projectID, true)
		return
	}
	if h.templatePreview == nil {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrContentTemplatePreviewNotAssembled))
		return
	}
	entityType := strings.TrimSpace(c.PostForm("entityType"))
	entityID := strings.TrimSpace(c.PostForm("entityId"))
	if entityType == "" || entityID == "" {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrPreviewParamsIncomplete))
		return
	}
	h.renderTemplatePreview(c, templateID, entityType, entityID, projectID, document, true)
}

// renderStructureTemplatePreview 无样例实体模式下的模板预览（结构模板：页眉 / 页脚）。
//
// 为什么走 page 编译管线（CompilePreview，与页面 / 全局块画布同一入口）而不是
// presentation.PreviewInstance —— 这个选择决定将来别人给结构模板加能力时的走向：
//
//  1. PreviewInstance 整条链是**以实体为前提**的：ValidateFieldRefs 按 entityType 校验、
//     registry.ResolverFor(entityType, entityID) 取实体解析器、applyEntitySEO 读实体字段。
//     在那条链上开一个「跳过」分支，等于让后续任何一处新增的实体依赖在无实体模式下静默
//     落空（页眉渲染成空），而结构模板**根本不是实体实例**，不该被塞进实体实例的路径。
//  2. 页面编译管线本身就**没有实体解析器**（Page Document 从不含字段绑定），它的 Compile
//     选项正是结构模板在正式构建里所用的那一份：同一 builder.Compile、同一组件集 / 插件
//     装配 / 主题 / 站点级选项 / 结构槽位展开 / 内容翻译 / ClientAsset。正式构建里结构模板
//     也是以 root 节点进入同一个 builder.Compile 的（pipeline.BuildStructureSlots）。
//  3. 字段绑定**跳过而不是伪造**：这里不注入、也不伪造实体解析器。真出现绑定（旧数据 /
//     绕过保存期校验的写入），编译器会按「解析器未注入」直接报错 —— 可见的失败，而不是
//     渲染成一片空白的假通过。结构模板不许有绑定这条不变量因此没有被削弱。
//
// currentPath 传空：结构模板文档没有自己的访问路径（它只作为槽位出现在引用页里）。
func (h *Handle) renderStructureTemplatePreview(c *gin.Context, document json.RawMessage,
	projectID string, withEditorBridge bool) {
	if h.pages == nil {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrStructureTemplatePreviewNotAssembled))
		return
	}
	if len(document) == 0 {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrTemplateDocumentEmpty))
		return
	}
	h.renderPreview(c, document, projectID, "", withEditorBridge, workbenchservice.PreviewDocStructureTemplate)
}

func (h *Handle) renderTemplatePreview(c *gin.Context, templateID, entityType, entityID, projectID string,
	draftDocument json.RawMessage, withEditorBridge bool) {
	res, err := h.templatePreview.PreviewInstance(c.Request.Context(), &presentationdto.PreviewInstanceReq{
		EntityType: entityType, EntityID: entityID, TemplateID: templateID,
		ProjectID: projectID, DraftDocument: draftDocument,
	})
	if err != nil {
		// 422 保留（编译失败对作者是业务信息），错误原文只进日志：
		// 编译器的错误里带节点路径与模板片段，直接铺在页面上等于把内部结构公开。
		logger.Scene("content_template").With("path", c.Request.URL.Path).Error(err, "模板编译失败")
		// 可归因的几类（模板类型串用 / 字段绑定越界 / 工程作用域没定下来）给能照着做的文案，
		// 其余归口：分类判据见 service.TemplatePreviewFacingKey。
		if key, ok := workbenchservice.TemplatePreviewFacingKey(err.Error()); ok {
			if text := workbenchFacingText(c, key); text != "" {
				c.String(http.StatusUnprocessableEntity, text)
				return
			}
		}
		c.String(http.StatusUnprocessableEntity, workbenchCompileFallbackText(c))
		return
	}
	html := res.HTML
	if withEditorBridge {
		html = injectEditorBridge(html, shell.TranslateFor(c))
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

// SetInstanceOverrideDeps 注入实例编辑模式端口（装配期调用；未注入时页面明确提示）。
func (h *Handle) SetInstanceOverrideDeps(instances presentationcontract.PresentationService) {
	h.instances = instances
}

// workbenchInstance 实例编辑模式外壳。
func (h *Handle) workbenchInstance(c *gin.Context, instanceID string) {
	if h.instances == nil {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrInstanceEditNotAssembled))
		return
	}
	ctx := c.Request.Context()
	inst, err := h.instances.Get(ctx, &presentationdto.GetReq{ID: instanceID, ProjectID: c.Query("projectId")})
	if err != nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrInstanceNotFound))
		return
	}
	// 生效文档：override 优先（toResp 随快照返回），否则实例尚无可编辑文档，
	// 由调用方（商品编辑页）先经 CreateInstance/Rebuild 产生快照再进来。
	document := inst.Document
	if len(document) == 0 {
		// 422 保留（这是「这个实例还不能编辑」的业务状态，不是服务端故障），
		// 文案走 key：硬编码中文在英文站点上不会翻译，且说不清下一步怎么做。
		c.String(http.StatusUnprocessableEntity, workbenchFacingText(c, workbenchenums.ErrInstanceNoDocument))
		return
	}
	documentJSON, _ := json.Marshal(document)
	// 渲染模式（迁移 282，双轨）：状态条据此显示「跟随模板中 / 独立文档」——
	// 编辑者必须随时知道自己在改的是共享模板还是这一个商品，否则会以为
	// 改模板会影响全站（或反之）。
	renderMode := strings.TrimSpace(inst.RenderMode)
	if renderMode != presentationdto.RenderModeDocument {
		renderMode = presentationdto.RenderModeTemplate
	}
	renderModeLabel := workbenchShortText(c, workbenchenums.ModeFollowTemplate)
	if renderMode == presentationdto.RenderModeDocument {
		renderModeLabel = workbenchShortText(c, workbenchenums.ModeDocument)
	}
	metaJSON, _ := json.Marshal(gin.H{
		"target": workbenchTargetOf(EditTargetInstance), "pageId": inst.ID,
		"saveBase": "instance", "entityType": inst.EntityType, "entityId": inst.EntityID,
		"projectId": inst.ProjectID, "draftPath": "", "instanceMode": true,
		"renderMode": renderMode,
	})
	schemas, serr := builder.ComponentSchemas()
	if serr != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaBuildFailed))
		return
	}
	schemasJSON, _ := json.Marshal(schemas)
	// 与另外三种画布模式一致地走 shell.Prepare：它注入 t（画布模板的取词函数）、
	// csrf_token（workbench.js 的 POST fetch 读它）与 lang。此前这个分支直接传 gin.H，
	// 于是 layout.html 的 `{{ .["csrf_token"] }}` 恒为空串 —— JS 只能靠 sessionStorage 兜底。
	c.HTML(http.StatusOK, "workbench/layout", shell.Prepare(c, gin.H{
		"title": workbenchShortText(c, workbenchenums.TitleInstance), "pageId": inst.ID, "isBlock": false, "isTemplate": true,
		// 双轨状态条（layout.html 据 isset(.instanceMode) 渲染）
		"instanceMode": true, "renderMode": renderMode, "renderModeLabel": renderModeLabel,
		"document": string(documentJSON), "meta": string(metaJSON), "schemas": string(schemasJSON),
		"previewQS": "instance=" + inst.ID + "&entityType=" + inst.EntityType +
			"&entityId=" + inst.EntityID + "&editor=1&projectId=" + inst.ProjectID,
		// jsVer 是 layout.html 的**必需键**（脚本 / 样式 / Trix 的缓存版本）：另外三种模式
		//（页面 / 块 / 模板）都给，唯独这里漏了。漏掉的后果与缺陷 B1 同一类，但更早 ——
		// 模板第 17 行就取它，于是实例编辑器整页在 <head> 里中断，只回 445 字节。
		// 回归守卫：public/test/workbench/feature/workbench_layout_render_test.go 的「实例模式」。
		"jsVer": workbenchservice.StaticJSVersion(),
	}))
}

// InstanceSave POST /workbench/instance/save：保存实例覆盖文档并重建发布。
// 只改本实例（override_document + 重编译发布），不影响共享模板（docs/04-C）。
func (h *Handle) InstanceSave(c *gin.Context) {
	if h.instances == nil {
		// 形态与同文件其余分支一致（c.JSON + code/message）：本端点由前端 fetch 消费，
		// api.js 的 send() 直接 r.json()，正文是 text/plain 时解析抛异常 → then 链断掉 →
		// 到不了那句 alert，用户看不到任何提示。文案走归口译文：能力未装配属装配缺陷，
		// 给用户看的只能是受控提示，装配细节不进响应。
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": shell.PageInternalText(c)})
		return
	}
	var body struct {
		ID            string          `json:"id"`
		ProjectID     string          `json:"projectId"`
		DraftDoc      json.RawMessage `json:"draftDocument"`
		ConfirmDetach bool            `json:"confirmDetach"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.ID) == "" || len(body.DraftDoc) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": workbenchShortText(c, workbenchenums.ErrSaveParamsIncomplete)})
		return
	}
	req := &presentationdto.SaveOverrideReq{
		InstanceID: strings.TrimSpace(body.ID),
		ProjectID:  body.ProjectID,
		Document:   body.DraftDoc,
		// 前端在收到 409（会放弃模板同步）并确认后重试时带上它。
		ConfirmDetach: body.ConfirmDetach,
	}
	res, err := h.instances.SaveOverrideDocument(c.Request.Context(), req)
	if err != nil {
		// 需要确认才能转入独立文档：回 409 + JSON，由前端弹确认后重试。
		// 顺序是「先请求、再确认」，因为判据（文档结构是否真的变了）只有服务端算得准。
		if strings.TrimSpace(err.Error()) == presentationenums.ErrDetachConfirmRequired {
			c.JSON(http.StatusConflict, gin.H{
				"code":    409,
				"message": workbenchShortText(c, workbenchenums.ErrDetachConfirmRequired),
			})
			return
		}
		// 其余失败：编译/校验原文带节点路径与模板片段，只进日志。
		// 能归因的（编译失败 / 工程作用域没定下来）给一条说清「没写入」与「怎么修」的文案，
		// 归不了因的给归口文案 —— 判据见 service.InstanceSaveFacingKey。
		logger.Scene("workbench").With("path", c.Request.URL.Path).Error(err, "实例文档保存失败")
		message := workbenchCompileFallbackText(c)
		if key, ok := workbenchservice.InstanceSaveFacingKey(err.Error()); ok {
			if text := workbenchFacingText(c, key); text != "" {
				message = text
			}
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": 422, "message": message})
		return
	}
	mode := presentationdto.RenderModeTemplate
	if res != nil && res.RenderMode != "" {
		mode = res.RenderMode
	}
	// data.renderMode 让前端保存成功后即时把状态条切成「独立文档」。
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "", "data": gin.H{"renderMode": mode}})
}

// workbench_preview_handle.go - 工作台预览渲染（页面 / 块草稿编译直出）。

// Preview 基于已保存草稿轻量编译并在独立响应中输出完整 HTML 文档
// （0-A1 §4.2 隔离预览：不落盘、不影响线上产物）。
func (h *Handle) Preview(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("id"))
	if pageID == "" {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrMissingPageID))
		return
	}
	page, err := h.svc.PageByID(c.Request.Context(), pageID)
	if err != nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrPageNotFound))
		return
	}
	h.renderPreview(c, page.DraftDocument, page.ProjectID, page.DraftPath, c.Query("editor") == "1", workbenchservice.PreviewDocPage)
}

// BlockPreview 全局块画布预览（工作台块编辑模式 iframe 内嵌）。
func (h *Handle) BlockPreview(c *gin.Context) {
	blockID := strings.TrimSpace(c.Query("id"))
	if blockID == "" {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrMissingBlockID))
		return
	}
	block, err := h.svc.BlockByID(c.Request.Context(), blockID)
	if err != nil || block == nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrBlockNotFound))
		return
	}
	h.renderPreview(c, block.Document, block.ProjectID, "", c.Query("editor") == "1", workbenchservice.PreviewDocBlock)
}

// PreviewDraft 基于未保存 AST 返回临时预览，不持久化、不写 Artifact、不影响发布指针。
func (h *Handle) PreviewDraft(c *gin.Context) {
	pageID := strings.TrimSpace(c.PostForm("id"))
	document := json.RawMessage(c.PostForm("draftDocument"))
	version, err := strconv.ParseInt(c.PostForm("expectedVersion"), 10, 64)
	if pageID == "" || err != nil || len(document) == 0 {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrDraftDecodeFailed))
		return
	}
	page, err := h.svc.PageByID(c.Request.Context(), pageID)
	if err != nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrPageNotFound))
		return
	}
	if version != page.DraftVersion {
		c.String(http.StatusConflict, workbenchShortText(c, workbenchenums.ErrDraftVersionStale))
		return
	}
	h.renderPreview(c, document, page.ProjectID, page.DraftPath, true, workbenchservice.PreviewDocPage)
}

// renderPreview 只完成 AST 校验与编译，响应生命周期结束即丢弃结果。
// 编译复用 page 模块 CompilePreview（与正式构建同源装配管线，docs/06 §10）：
// 全局块引用展开、插件组件集注入、主题/集合解析均与构建一致，画布所见即产物。
// editorBridge（画布联动 JS）为后处理拼接，与编译无关，仅预览启用。
// projectID 为文档所属站点工程（页面/块的记录字段），驱动导航等站点级资源解析；
// currentPath 为页面访问路径（导航当前项高亮，块预览传空）。
// kind 标识预览文档的来源（页面 / 全局块 / 结构模板）：422 的可归因文案按它分流，
// 因为同一句「编译失败」在三种画布上的下一步动作并不相同（见 workbench_err.go）。
//
// 编译失败一律走 writePreviewCompileRejected：状态码仍是 422（对作者是业务信息，
// 不是服务端故障），但**错误原文不再进响应** —— 它带节点路径与模板片段，
// 只进结构化日志；对外是三条可归因文案 + 一条归口文案。
// 代价要写在这里：原先透出的「轮播至少需要一个 slide」这类逐组件提示不再出现在
// 画布上（原文仍在日志里），换成了分类文案里说清的「怎么办」。这是按「原文只进
// 日志」的纪律收的，若将来要恢复逐组件提示，正确做法是让验证器返回**结构化**的
// 问题列表（组件 + 槽位 + 处置），而不是把 err.Error() 拼回响应。
func (h *Handle) renderPreview(c *gin.Context, document json.RawMessage, projectID, currentPath string,
	withEditorBridge bool, kind workbenchservice.PreviewDocKind) {
	// 预览语言：?lang= 显式指定（工作台多语言预览切换），空 = 站点默认语言。
	// 画布标记层与画布联动脚本同开关：两者都只对编辑器有意义，普通预览（编辑器外链预览）
	// 不该多出那一层 div —— 它会让「预览 HTML」与产物 HTML 不再是同一份结构。
	//
	// 编译与分级全在 service.RenderPreview（html + outcome + err 三返回值）；
	// 本函数只把 outcome 翻成状态码与响应体，分类判据（哪种失败给哪条文案）在 service 里。
	html, outcome, err := h.svc.RenderPreview(c.Request.Context(), document, projectID, currentPath,
		c.Query("lang"), withEditorBridge)
	switch outcome {
	case workbenchservice.PreviewOK:
		if withEditorBridge {
			html = []byte(injectEditorBridge(string(html), shell.TranslateFor(c)))
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", html)
	case workbenchservice.PreviewInvalidDocument:
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrDraftDecodeFailed))
	case workbenchservice.PreviewCompileRejected:
		writePreviewCompileRejected(c, document, kind, err)
	default:
		// 这里原先直写 workbenchenums.MsgInternalError —— 那是一个裸 key，
		// 画布响应体不经过 pkg/response 的翻译层，浏览器里看到的就是 "MsgInternalError"。
		c.String(http.StatusInternalServerError, shell.PageInternalText(c))
	}
}

// editorBridgeScript 在 iframe 内运行的编辑器桥接脚本（仅编辑器预览注入）。
// 职责：节点选择标记还原、点击选中上报、选中高亮、容器/元素下方
// 「+ 插入组件」浮标、拖放落点指示线样式。
const editorBridgeScript = `<script>
(function(){
  // 编译器把节点 ID 编入 sky-c-* CSS 类；编辑器桥接层将其还原为选择标记。
  //
  // 前缀长度一律取 WB_SKY_PREFIX.length，不写死数字：这里曾是 slice(5)，
  // 而 'sky-c-' 是 6 个字符 —— 每个节点的 data-sky-id 都多出一个前导 "-"，
  // 于是画布发回父窗口的每一条消息（选中 / 直改文本 / 右键操作 / 拖放重排 /
  // 就地插入）带的都是 findNode 查不到的 id：双击能进入编辑态，失焦后
  // 回写被静默丢弃（文字弹回），点选也毫无反应。
  var WB_SKY_PREFIX = 'sky-c-';
  document.querySelectorAll('[class]').forEach(function(el){
    el.classList.forEach(function(cls){
      if (cls.indexOf(WB_SKY_PREFIX) !== 0) return;
      el.setAttribute('data-sky-id', cls.slice(WB_SKY_PREFIX.length));
    });
  });
  document.querySelectorAll('[id]').forEach(function(el){
    if (!el.getAttribute('data-sky-id')) el.setAttribute('data-sky-id', el.id);
  });
  document.querySelectorAll('[data-sky-id]').forEach(function(el){ el.setAttribute('draggable', 'true'); });

  // 结构槽位（页眉 / 页脚）在画布里是**只读边界**，不是可编辑节点。
  //
  // 它的 data-sky-id（__layout_header）在页面 AST 里并不存在 —— 槽位是编译期注入的，
  // 画布按 AST 查不到它，于是拖动 / 双击改文本 / 右键菜单对它全是「消息发出去没人认」
  // 的静默失败。这里把它单独标出来：不可拖，点击上报 wb-slot-select，
  // 由父窗口打开槽位面板（点进去编辑的是**全局块**，与 WP 的 header 模板同一范式）。
  document.querySelectorAll('[data-sky-slot], [data-sky-slot-frame]').forEach(function(el){
    el.setAttribute('draggable', 'false');
    el.classList.add('wb-slot');
  });

  // 画布内元素可直接拖动重排：与大纲树/组件库共用同一数据键。
  // 拖放落点在本桥接内计算（iframe 每次刷新必然重新注入，
  // 不依赖父窗口绑定时序），通过 wb-canvas-drop 消息交父窗口执行 AST 变更。
  var dropCtx = null;
  function clearDropMarks(){
    document.querySelectorAll('.wb-drop-before,.wb-drop-after,.wb-drop-inside').forEach(function(el){
      el.classList.remove('wb-drop-before','wb-drop-after','wb-drop-inside');
    });
  }
  document.addEventListener('dragstart', function(ev){
    // 槽位子树（页眉 / 页脚）不属于本页 AST：拖它只会得到一次无人响应 moveNode。
    if (ev.target.closest && ev.target.closest('[data-sky-slot]')) return;
    var target = ev.target.closest ? ev.target.closest('[data-sky-id]') : null;
    if(!target) return;
    ev.dataTransfer.effectAllowed = 'move';
    ev.dataTransfer.setData('application/x-wb-node', target.getAttribute('data-sky-id'));
    target.style.opacity = '0.4';
    setTimeout(function(){ target.style.opacity = ''; }, 0);
  }, true);
  document.addEventListener('dragover', function(ev){
    // 槽位子树不接收落点：往里插组件等于往「站点结构里那份块」插，而这里改的是本页文档。
    if (ev.target.closest && ev.target.closest('[data-sky-slot]')) { clearDropMarks(); dropCtx = null; return; }
    var target = ev.target.closest ? ev.target.closest('[data-sky-id]') : null;
    clearDropMarks();
    if(!target) return;
    ev.preventDefault();
    var rect = target.getBoundingClientRect();
    var offset = ev.clientY - rect.top;
    var inMiddle = offset > rect.height * .3 && offset < rect.height * .7;
    var placement = inMiddle ? 'inside' : (offset < rect.height / 2 ? 'before' : 'after');
    // 容器判定由父窗口按 AST 进行；桥接按「有子元素且中带」粗略显示内部虚线。
    target.classList.add(placement === 'inside' ? 'wb-drop-inside' : (placement === 'before' ? 'wb-drop-before' : 'wb-drop-after'));
    dropCtx = { targetID: target.getAttribute('data-sky-id'), placement: placement, inMiddle: inMiddle, hasChildren: target.children.length > 0 };
  });
  document.addEventListener('dragleave', function(ev){
    if (!ev.relatedTarget) { clearDropMarks(); dropCtx = null; }
  });
  document.addEventListener('drop', function(ev){
    ev.preventDefault();
    clearDropMarks();
    var componentType = ev.dataTransfer.getData('application/x-wb-component');
    if (componentType) {
      // 组件库拖入：DataTransfer 归父窗口所有，交父窗口 bindCanvasDrop 处理。
      return;
    }
    var nodeID = ev.dataTransfer.getData('application/x-wb-node');
    if (!nodeID) return;
    var ctx = dropCtx || {};
    dropCtx = null;
    parent.postMessage({
      type: 'wb-canvas-drop',
      nodeID: nodeID,
      targetID: ctx.targetID || '',
      placement: ctx.placement || 'after',
      inMiddle: !!ctx.inMiddle,
      hasChildren: !!ctx.hasChildren
    }, location.origin);
  });

  var style = document.createElement('style');
  style.textContent = [
    '[data-sky-id]:hover{outline:1px solid rgba(37,99,235,.45);outline-offset:-1px;cursor:pointer;}',
    '[data-sky-id].wb-selected{outline:2px solid #2563eb;outline-offset:-2px;}',
    '.wb-bridge-insert{',
    '  position:absolute;z-index:99998;left:50%;transform:translateX(-50%);',
    '  padding:4px 12px;font-size:12px;line-height:1.6;white-space:nowrap;',
    '  color:#fff;background:#2563eb;border:none;border-radius:999px;cursor:pointer;',
    '  box-shadow:0 2px 10px rgba(37,99,235,.45);',
    '}',
    '.wb-bridge-insert:hover{background:#1d4ed8;}',
    '[data-sky-id].wb-drop-before{box-shadow:0 -3px 0 0 #2563eb;}',
    '[data-sky-id].wb-drop-after{box-shadow:0 3px 0 0 #2563eb;}',
    '[data-sky-id].wb-drop-inside{outline:2px dashed #2563eb;outline-offset:-2px;}',
    // 结构槽位：紫色虚线边界，与普通组件的蓝色区分开 —— 它不是本页的节点。
    '[data-sky-slot].wb-slot{outline:1px dashed rgba(124,58,237,.5);outline-offset:-1px;}',
    '[data-sky-slot].wb-slot:hover{outline:2px dashed #7c3aed;}',
    '[data-sky-slot].wb-selected{outline:2px solid #7c3aed;outline-offset:-2px;}'
  ].join('');
  document.head.appendChild(style);

  document.addEventListener('click', function(ev){
    // 槽位优先：点页眉 / 页脚（含它们内部的内容）走的不是「选中本页节点」，
    // 而是「这段 DOM 属于站点结构」——父窗口据此打开槽位面板。
    var slotEl = ev.target.closest ? ev.target.closest('[data-sky-slot]') : null;
    if (slotEl) {
      ev.preventDefault(); ev.stopPropagation();
      parent.postMessage({
        type: 'wb-slot-select',
        slot: slotEl.getAttribute('data-sky-slot') || '',
        ref: slotEl.getAttribute('data-sky-slot-ref') || slotEl.getAttribute('data-sky-ref') || '',
        refKind: slotEl.getAttribute('data-sky-slot-ref-kind') || '',
        id: slotEl.getAttribute('data-sky-id') || ''
      }, location.origin);
      return;
    }
    var target = ev.target.closest('[data-sky-id]');
    if(!target) return;
    ev.preventDefault(); ev.stopPropagation();
    parent.postMessage({type:'wb-select', id: target.getAttribute('data-sky-id')}, location.origin);
  }, true);

  // 「+ 插入组件」浮标：父窗口在选中变化时发 wb-mark-selected，
  // 此处把浮标定位到选中元素底部中央；点击上报插入意图，
  // 由父窗口根据 AST 判断目标是容器(inside)还是普通元素(after)。
  var insertBtn = document.createElement('button');
  insertBtn.type = 'button';
  insertBtn.className = 'wb-bridge-insert';
  insertBtn.textContent = '+ {{bridge.insert}}';
  insertBtn.style.display = 'none';
  document.body.appendChild(insertBtn);
  insertBtn.addEventListener('click', function(ev){
    ev.preventDefault(); ev.stopPropagation();
    var id = insertBtn.getAttribute('data-target-id') || '';
    if (id) parent.postMessage({type:'wb-insert-here', id: id}, location.origin);
  });

  window.addEventListener('message', function(ev){
    if (ev.origin !== location.origin || !ev.data) return;
    if (ev.data.type === 'wb-mark-selected') {
      var prev = document.querySelector('[data-sky-id].wb-selected');
      if (prev) prev.classList.remove('wb-selected');
      var el = ev.data.id ? document.querySelector('[data-sky-id="' + ev.data.id + '"]') : null;
      if (el) {
        el.classList.add('wb-selected');
        var rect = el.getBoundingClientRect();
        // 槽位不挂「+ 插入组件」：它内部的内容属于全局块，不归本页文档。
        if (el.hasAttribute('data-sky-slot')) {
          insertBtn.style.display = 'none';
        } else {
          insertBtn.style.display = 'block';
          insertBtn.setAttribute('data-target-id', ev.data.id);
          insertBtn.style.top = (rect.bottom + window.scrollY + 4) + 'px';
        }
      } else {
        insertBtn.style.display = 'none';
      }
    }
  });

  // ========== 画布直改三件套（对标 Figma/Elementor 就地编辑） ==========

  // 1) 双击就地编辑：文本类组件（heading/text/button/card 等）双击 →
  //    contenteditable 就地编辑 → 失焦/回车回写 AST（wb-edit-text 消息）。
  document.addEventListener('dblclick', function(ev){
    // 槽位（页眉 / 页脚）里的文本不能就地改：改的必须是**全局块**，
    // 否则页内副本会与站点结构那份分叉（就是「两个页眉」那条老路）。
    if (ev.target.closest && ev.target.closest('[data-sky-slot]')) return;
    var target = ev.target.closest('[data-sky-id]');
    if(!target) return;
    ev.preventDefault(); ev.stopPropagation();
    // 已在编辑中不重复进入。
    if (target.isContentEditable) return;
    target.setAttribute('contenteditable', 'plaintext-only');
    target.focus();
    // 全选文本（就地替换习惯）。
    var range = document.createRange();
    range.selectNodeContents(target);
    var sel = window.getSelection();
    sel.removeAllRanges(); sel.addRange(range);
    target.classList.add('wb-editing');
    function finish(save){
      target.removeAttribute('contenteditable');
      target.classList.remove('wb-editing');
      target.removeEventListener('blur', onBlur);
      target.removeEventListener('keydown', onKey);
      if (save) {
        parent.postMessage({
          type: 'wb-edit-text',
          id: target.getAttribute('data-sky-id'),
          text: target.textContent.trim()
        }, location.origin);
      } else {
        // 取消：下次画布刷新自动还原（不主动刷新，等下次交互）。
      }
    }
    function onBlur(){ finish(true); }
    function onKey(e){
      if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); target.blur(); }
      if (e.key === 'Escape') { e.preventDefault(); finish(false); }
    }
    target.addEventListener('blur', onBlur);
    target.addEventListener('keydown', onKey);
  });

  // 2) 画布右键菜单：编辑/复制/粘贴到内部/删除/上移/下移/隐藏 + 动效快捷项。
  var ctxMenu = null;
  function closeCtxMenu(){ if (ctxMenu) { ctxMenu.remove(); ctxMenu = null; } }
  document.addEventListener('contextmenu', function(ev){
    var slotHit = ev.target.closest ? ev.target.closest('[data-sky-slot]') : null;
    closeCtxMenu();
    if (slotHit) {
      // 槽位没有「复制 / 删除 / 上下移」这类本页操作：它只有「去改那个块」。
      ev.preventDefault(); ev.stopPropagation();
      parent.postMessage({
        type: 'wb-slot-select',
        slot: slotHit.getAttribute('data-sky-slot') || '',
        ref: slotHit.getAttribute('data-sky-slot-ref') || slotHit.getAttribute('data-sky-ref') || '',
        refKind: slotHit.getAttribute('data-sky-slot-ref-kind') || '',
        id: slotHit.getAttribute('data-sky-id') || ''
      }, location.origin);
      return;
    }
    var target = ev.target.closest('[data-sky-id]');
    if(!target) return; // 画布空白处不拦截（浏览器原生菜单）。
    ev.preventDefault(); ev.stopPropagation();
    var id = target.getAttribute('data-sky-id');
    parent.postMessage({type:'wb-select', id: id}, location.origin);
    ctxMenu = document.createElement('div');
    ctxMenu.className = 'wb-ctx-menu';
    function item(label, action){
      var b = document.createElement('button');
      b.type = 'button'; b.textContent = label;
      b.addEventListener('click', function(e){ e.stopPropagation(); closeCtxMenu(); action(); });
      ctxMenu.appendChild(b);
    }
    function separator(){ var s = document.createElement('div'); s.className='wb-ctx-sep'; ctxMenu.appendChild(s); }
    function send(msg){ parent.postMessage(msg, location.origin); }
    item('✏️ {{bridge.editText}}', function(){ // 触发双击编辑。
      var el = document.querySelector('[data-sky-id="' + id + '"]');
      if (el) { var d = new MouseEvent('dblclick', {bubbles:true}); el.dispatchEvent(d); }
    });
    item('⧉ {{bridge.copy}}', function(){ send({type:'wb-ctx', id:id, op:'copy'}); });
    item('✂ {{bridge.cut}}', function(){ send({type:'wb-ctx', id:id, op:'cut'}); });
    item('📋 {{bridge.pasteInside}}', function(){ send({type:'wb-ctx', id:id, op:'paste-inside'}); });
    separator();
    item('↑ {{bridge.moveUp}}', function(){ send({type:'wb-ctx', id:id, op:'move-up'}); });
    item('↓ {{bridge.moveDown}}', function(){ send({type:'wb-ctx', id:id, op:'move-down'}); });
    separator();
    // 动效快捷子项（效果基本库入口：常用 4 种入场 + 悬浮）。
    var anim = document.createElement('div'); anim.className='wb-ctx-group'; anim.textContent='✨ {{bridge.entranceGroup}}';
    ctxMenu.appendChild(anim);
    ['fade-up','zoom-in','slide-up','blur-in'].forEach(function(eff){
      item('　' + eff, function(){ send({type:'wb-ctx', id:id, op:'entrance', value:eff}); });
    });
    item('🌀 {{bridge.hoverLift}}', function(){ send({type:'wb-ctx', id:id, op:'hover', value:'lift'}); });
    separator();
    item('🗑 {{bridge.delete}}', function(){ send({type:'wb-ctx', id:id, op:'delete'}); });
    document.body.appendChild(ctxMenu);
    // 定位（不越界）。
    var x = Math.min(ev.pageX, window.innerWidth - 180);
    var y = Math.min(ev.pageY, window.innerHeight - 320);
    ctxMenu.style.left = x + 'px'; ctxMenu.style.top = y + 'px';
  });
  document.addEventListener('click', function(ev){
    if (ctxMenu && !ctxMenu.contains(ev.target)) closeCtxMenu();
  }, true);

  // 3) 选中悬浮快捷条（Elementor 式小工具条：编辑/复制/删除）。
  var quickBar = document.createElement('div');
  quickBar.className = 'wb-quick-bar';
  quickBar.style.display = 'none';
  document.body.appendChild(quickBar);
  function positionQuickBar(el){
    var rect = el.getBoundingClientRect();
    quickBar.style.display = 'flex';
    quickBar.style.left = rect.left + 'px';
    quickBar.style.top = (rect.top - 30 + window.scrollY) + 'px';
    quickBar.setAttribute('data-target-id', el.getAttribute('data-sky-id'));
  }
  window.addEventListener('message', function(ev){
    if (ev.origin !== location.origin || !ev.data) return;
    if (ev.data.type === 'wb-mark-selected') {
      var el = ev.data.id ? document.querySelector('[data-sky-id="' + ev.data.id + '"]') : null;
      // 槽位的快捷条没有意义（没有「复制本页副本 / 删除」这类操作），不显示。
      if (el && !el.hasAttribute('data-sky-slot')) positionQuickBar(el); else quickBar.style.display = 'none';
    }
  });
  [['✏️','{{bridge.editText}}',function(){ var el=document.querySelector('[data-sky-id="'+quickBar.getAttribute('data-target-id')+'"]'); if(el) el.dispatchEvent(new MouseEvent('dblclick',{bubbles:true})); }],
   ['⧉','{{bridge.copy}}',function(){ send2({type:'wb-ctx', id:quickBar.getAttribute('data-target-id'), op:'copy'}); }],
   ['🗑','{{bridge.delete}}',function(){ send2({type:'wb-ctx', id:quickBar.getAttribute('data-target-id'), op:'delete'}); }]
  ].forEach(function(t){
    var b = document.createElement('button');
    b.type='button'; b.textContent=t[0]; b.title=t[1];
    b.addEventListener('click', function(e){ e.stopPropagation(); t[2](); });
    quickBar.appendChild(b);
  });
  function send2(msg){ parent.postMessage(msg, location.origin); }

  // 直改样式（右键菜单/快捷条/编辑态）。
  var directStyle = document.createElement('style');
  directStyle.textContent = [
    '[data-sky-id].wb-editing{outline:2px solid #3d444f !important;cursor:text;}',
    '[contenteditable]{outline-offset:-2px;}',
    '.wb-ctx-menu{position:absolute;z-index:99999;min-width:160px;background:#fff;',
    '  border:1px solid #e5e7eb;border-radius:8px;box-shadow:0 8px 24px rgba(0,0,0,.14);',
    '  padding:4px;font-size:13px;color:#1a1d21;}',
    '.wb-ctx-menu button{display:block;width:100%;text-align:left;padding:6px 10px;',
    '  border:none;background:none;cursor:pointer;border-radius:6px;font-size:13px;color:inherit;}',
    '.wb-ctx-menu button:hover{background:#eceef1;}',
    '.wb-ctx-sep{height:1px;background:#e5e7eb;margin:4px 0;}',
    '.wb-ctx-group{padding:6px 10px 2px;font-size:11px;color:#6b7280;font-weight:600;}',
    '.wb-quick-bar{position:absolute;z-index:99998;display:none;gap:2px;',
    '  background:#1a1d21;border-radius:6px;padding:3px;box-shadow:0 4px 12px rgba(0,0,0,.25);}',
    '.wb-quick-bar button{border:none;background:none;cursor:pointer;font-size:13px;',
    '  padding:4px 8px;border-radius:4px;color:#fff;}',
    '.wb-quick-bar button:hover{background:rgba(255,255,255,.15);}'
  ].join('');
  document.head.appendChild(directStyle);

  // 局部刷新（父窗口 wb-patch）：只替换目标节点的 DOM 与整页样式，不重载 iframe。
  window.addEventListener('message', function (ev) {
    if (ev.origin !== location.origin || !ev.data || ev.data.type !== 'wb-patch') return;
    var el = document.querySelector('[data-sky-id="' + ev.data.id + '"]');
    if (el && ev.data.html) {
      var tmp = document.createElement('div');
      tmp.innerHTML = ev.data.html;
      var fresh = tmp.firstElementChild;
      if (fresh) {
        fresh.setAttribute('data-sky-id', ev.data.id);
        fresh.setAttribute('draggable', 'true');
        if (el.classList.contains('wb-selected')) fresh.classList.add('wb-selected');
        el.replaceWith(fresh);
      }
    }
    if (typeof ev.data.css === 'string') {
      var st = document.getElementById('wb-live-css');
      if (!st) { st = document.createElement('style'); st.id = 'wb-live-css'; document.head.appendChild(st); }
      st.textContent = ev.data.css;
    }
  });

  // 拖放落点指示：父窗口 bindCanvasDrop 在 dragover 时给目标加类，
  // 这里只负责样式；drop/dragleave 时父窗口负责移除。
})();
</script>`

// editorBridgeTexts 桥接脚本里的可见文案：占位符名 → 词条 key + 中文兜底。
//
// 脚本以 `{{bridge.<name>}}` 书写、注入时替换，而不是把中文直接写在脚本里 ——
// 这些串会由 iframe 内的 JS 输出（浮标文字、右键菜单项、快捷条 title），
// 硬编码中文在英文画布上就是漏译。占位符形态还让「脚本里的占位符集合 =
// 本表的名字集合」成为可机器校验的判据（见 editor_bridge_test.go），
// 漏登记一个的表现是按钮上原样显示花括号，而这类缺陷不会让任何断言变红。
var editorBridgeTexts = []struct{ Name, Key, Fallback string }{
	{"bridge.insert", workbenchenums.BridgeInsert, "插入组件"},
	{"bridge.editText", workbenchenums.BridgeEditText, "编辑文本"},
	{"bridge.copy", workbenchenums.BridgeCopy, "复制"},
	{"bridge.cut", workbenchenums.BridgeCut, "剪切"},
	{"bridge.pasteInside", workbenchenums.BridgePasteInside, "粘贴到内部"},
	{"bridge.moveUp", workbenchenums.BridgeMoveUp, "上移"},
	{"bridge.moveDown", workbenchenums.BridgeMoveDown, "下移"},
	{"bridge.delete", workbenchenums.BridgeDelete, "删除"},
	{"bridge.entranceGroup", workbenchenums.BridgeEntranceGroup, "入场动画"},
	{"bridge.hoverLift", workbenchenums.BridgeHoverLift, "悬浮上浮"},
}

// editorBridgeScriptFor 按当前语言产出桥接脚本：占位符换成译文后返回完整 <script>。
func editorBridgeScriptFor(tr func(key, fallback string) string) string {
	pairs := make([]string, 0, len(editorBridgeTexts)*2)
	for _, it := range editorBridgeTexts {
		pairs = append(pairs, "{{"+it.Name+"}}", jsSingleQuoted(tr(it.Key, it.Fallback)))
	}
	return strings.NewReplacer(pairs...).Replace(editorBridgeScript)
}

// jsSingleQuoted 把译文转义成能放进单引号 JS 字符串字面量的形态。
//
// 词条是运营可改的数据，不是编译期常量：一个撇号（如 "Don't"）就会当场把脚本打断，
// 而断掉的后果是**画布里的桥接整体失效**（选中、拖放、右键全部无反应）——
// 报错在 iframe 控制台，父窗口看起来只是「点了没反应」。
func jsSingleQuoted(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		"\r", `\r`,
		"\n", `\n`,
		"</", `<\/`,
	).Replace(s)
}

// injectEditorBridge 把编辑器桥接脚本（按请求语言取词后）追加到 </body> 前。
func injectEditorBridge(html string, tr func(key, fallback string) string) string {
	idx := strings.LastIndex(html, "</body>")
	if idx < 0 {
		return html
	}
	return html[:idx] + editorBridgeScriptFor(tr) + html[idx:]
}

// workbench_target.go — 工作台编辑目标描述符（审计 EDT-017）。
//
// 工作台此前靠 meta.saveBase 字符串分派（page / block / template 各写一段 if）：
// 接入一种新文档类型要在保存、预览、校验、历史四处各加一个分支，而这些分派散在
// 前端 JS 里 —— 漏改一处不会编译失败，只会在用户点保存时表现为「什么都没发生」。
//
// 描述符把「这个目标有哪些端点、支持哪些动作、保存体长什么样」变成**数据**：
// 前端只按描述符走，新增目标类型 = 在这里注册一条，前端零改动。

// 编辑目标类型。
const (
	EditTargetPage     = "page"
	EditTargetBlock    = "block"
	EditTargetTemplate = "template"
	// EditTargetInstance 实例编辑模式（迁移 281，docs/04-C-instance-override.md）：
	// 画布改的是该实例自己的覆盖文档，保存只影响本实例。
	EditTargetInstance = "instance"
)

// TargetEndpoint 目标的一个端点（method 省略即 POST：项目的写操作一律 POST）。
type TargetEndpoint struct {
	Path string `json:"path"`
}

// TargetSaveBody 保存请求体的构造方式。
//
// 三种目标的键名各不相同（page 用 draftDocument + expectedVersion + draftPath，
// block 用 document 且额外带 name，template 用 draftDocument 无版本），
// 键名放数据里，前端就不必认识任何一种目标。
type TargetSaveBody struct {
	// IDKey 目标 id 的键名。
	IDKey string `json:"idKey"`
	// DocumentKey 文档的键名。
	DocumentKey string `json:"documentKey"`
	// VersionKey 乐观锁版本键（空 = 该目标不用版本）。
	VersionKey string `json:"versionKey,omitempty"`
	// PathKey 路径键（空 = 该目标没有 URL 概念）。
	PathKey string `json:"pathKey,omitempty"`
	// Extras 附加字段：请求体键 → meta 里的来源键（如 block 的 name ← blockName）。
	Extras map[string]string `json:"extras,omitempty"`
}

// TargetCapabilities 能力开关：前端据此显示或隐藏按钮。
//
// 没有这层的话，「块没有发布链」这种事只能靠前端记：按钮照常显示，点了报错，
// 或者更糟 —— 点了之后静默失败。
type TargetCapabilities struct {
	Publish  bool `json:"publish"`
	Build    bool `json:"build"`
	Rollback bool `json:"rollback"`
	History  bool `json:"history"`
	URL      bool `json:"url"`
}

// EditTarget 一种可编辑文档类型的完整描述。
type EditTarget struct {
	Type     string             `json:"type"`
	Save     TargetEndpoint     `json:"save"`
	SaveBody TargetSaveBody     `json:"saveBody"`
	Caps     TargetCapabilities `json:"caps"`
}

// workbenchTargets 目标注册表 —— **新增编辑目标只需要在这里加一条**。
var workbenchTargets = map[string]EditTarget{
	EditTargetPage: {
		Type: EditTargetPage,
		Save: TargetEndpoint{Path: "/api/page/draft/save"},
		SaveBody: TargetSaveBody{
			IDKey: "id", DocumentKey: "draftDocument",
			VersionKey: "expectedVersion", PathKey: "draftPath",
		},
		// 手工页面是唯一有完整发布链的目标（构建 / 发布 / 回滚 / 历史 / 改 URL）。
		Caps: TargetCapabilities{Publish: true, Build: true, Rollback: true, History: true, URL: true},
	},
	EditTargetBlock: {
		Type: EditTargetBlock,
		Save: TargetEndpoint{Path: "/admin/blocks/save-content"},
		SaveBody: TargetSaveBody{
			IDKey: "id", DocumentKey: "document",
			// returnUrl：菜单页「新建面板块并编辑」带过来的回跳目标，随保存请求体回传。
			// 白名单校验两处都做（这里只做搬运，消费侧 SaveBlockContent 再校验一次）——
			// 前端 meta 是可被改写的中间态，不能当成「已经校验过了」。
			Extras: map[string]string{"name": "blockName", "returnUrl": "returnUrl"},
		},
		// 全局块没有 URL、没有独立产物：它是被引用展开进别人的产物的。
		Caps: TargetCapabilities{},
	},
	EditTargetTemplate: {
		Type:     EditTargetTemplate,
		Save:     TargetEndpoint{Path: "/api/contenttemplate/update"},
		SaveBody: TargetSaveBody{IDKey: "id", DocumentKey: "draftDocument"},
		// 模板保存即产生新版本（无独立发布动作），也没有自己的 URL。
		Caps: TargetCapabilities{},
	},
	// 实例编辑模式（迁移 281，docs/04-C-instance-override.md）：保存只影响本实例
	//（覆盖文档 + 重编译发布），不产生模板新版本，也没有自己的 URL。
	EditTargetInstance: {
		Type: EditTargetInstance,
		Save: TargetEndpoint{Path: "/workbench/instance/save"},
		SaveBody: TargetSaveBody{
			IDKey: "id", DocumentKey: "draftDocument",
			Extras: map[string]string{"projectId": "projectId"},
		},
		Caps: TargetCapabilities{},
	},
}

// workbenchTargetOf 取目标描述符；类型未注册时 panic。
//
// panic 而不是返回空描述符：三个注入点写的都是常量类型，未注册只可能是
// 「加了新目标常量却忘了在注册表里加一条」—— 这种错误要在首次渲染就炸出来，
// 而不是把一个空描述符发给前端（前端会静默退回默认行为，问题看不见）。
func workbenchTargetOf(targetType string) EditTarget {
	target, ok := EditTargetFor(targetType)
	if !ok {
		panic("工作台编辑目标未注册: " + targetType)
	}
	return target
}

// EditTargetFor 按类型取描述符；未知类型返回 false（调用方据此明确报错，不静默降级）。
func EditTargetFor(targetType string) (EditTarget, bool) {
	t, ok := workbenchTargets[targetType]
	return t, ok
}

// WorkbenchTargets 返回全部目标的确定性列表（测试与前端契约共用）。
func WorkbenchTargets() []EditTarget {
	types := make([]string, 0, len(workbenchTargets))
	for t := range workbenchTargets {
		types = append(types, t)
	}
	sort.Strings(types)
	out := make([]EditTarget, 0, len(types))
	for _, t := range types {
		out = append(out, workbenchTargets[t])
	}
	return out
}

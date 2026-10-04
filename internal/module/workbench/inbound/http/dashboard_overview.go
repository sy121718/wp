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
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	// overviewTrendDays 趋势图与热销榜的窗口（含当天）。
	overviewTrendDays = 7
	// overviewTopLimit 热销榜条数（与商品榜卡片的固定高度对应）。
	overviewTopLimit = 5
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
	TopProducts(ctx context.Context, req *orderdto.OrderTopProductsReq) (res *orderdto.OrderTopProductsResp, err error)
	StatusCounts(ctx context.Context, req *orderdto.OrderStatusCountsReq) (res *orderdto.OrderStatusCountsResp, err error)
}

// OverviewAnalyticsPort 概览页所需的访问统计只读面（一条方法）。
type OverviewAnalyticsPort interface {
	Summary(ctx context.Context, req *analyticsdto.SummaryReq) (res *analyticsdto.SummaryResp, err error)
}

// OverviewPageKindPort 路径 → 页面类型（判断哪些浏览发生在文章页上）。
type OverviewPageKindPort interface {
	KindsOfPaths(ctx context.Context, projectID string, paths []string) (map[string]string, error)
}

// overviewKPI 四张卡的数字（全部为**当天**口径）。
type overviewKPI struct {
	TodayOrders      int64
	TodaySalesCents  int64
	TodaySalesLabel  string
	ArticleViews     int64
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
	// HeightPct 0~100：模板不做算术，柱高在服务端算好（页面与 AI 都不会各算一份）。
	HeightPct int
}

// overviewTopProduct 榜单的一行（跨工程合并后重排名次）。
type overviewTopProduct struct {
	Rank        int
	ProductName string
	SKU         string
	Quantity    int64
	AmountLabel string
}

// overviewSnapshot 概览页的全部跨模块数据。
type overviewSnapshot struct {
	// OrdersReady / AnalyticsReady 表示对应端口**已接线**（不是「有数据」）：
	// 未接线时模板渲染空态并说明「暂不可用」，而不是显示一片 0 —— 0 会被当成真实统计。
	OrdersReady    bool
	AnalyticsReady bool
	Today          string
	From           string
	KPI            overviewKPI
	Trend          []overviewTrendPoint
	Top            []overviewTopProduct
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
func (h *Handle) SetOverviewPorts(orders OverviewOrderPort, analytics OverviewAnalyticsPort, pageKinds OverviewPageKindPort) {
	h.overviewOrders = orders
	h.overviewAnalytics = analytics
	h.overviewPageKinds = pageKinds
}

// collectOverview 汇总全部工程的概览数据（任一模块失败只影响它自己的块）。
func (h *Handle) collectOverview(ctx context.Context, projectIDs []string) overviewSnapshot {
	// UTC 日界：与 order 的按天桶、analytics 的按天聚合同口径（见文件头）。
	now := time.Now().UTC()
	today := now.Format(utils.LayoutDay)
	from := now.AddDate(0, 0, -(overviewTrendDays - 1)).Format(utils.LayoutDay)

	snap := overviewSnapshot{Today: today, From: from}
	h.collectOrderOverview(ctx, projectIDs, from, today, &snap)
	h.collectArticleViews(ctx, projectIDs, today, &snap)
	snap.KPI.TodaySalesLabel = moneyLabel(snap.KPI.TodaySalesCents)
	snap.PortsReady = snap.OrdersReady && snap.AnalyticsReady
	return snap
}

// collectOrderOverview 取订单侧的 KPI / 趋势 / 榜单（逐工程取回后在内存里累加）。
//
// 逐工程而不是一次全局查询：orders 带 FORCE 策略，聚合方法都要求显式工程 id
// （那正是「不许出现不限工程的查询」这条纪律的形状）。工程数量在个位数量级，
// 多几次查询换来的是「不可能读到别人的数据」。
func (h *Handle) collectOrderOverview(ctx context.Context, projectIDs []string, from, today string, snap *overviewSnapshot) {
	if h.overviewOrders == nil {
		return
	}
	snap.OrdersReady = true
	byDay := make(map[string]*overviewTrendPoint, overviewTrendDays)
	var products []overviewTopProduct

	for _, pid := range projectIDs {
		if res, err := h.overviewOrders.SummaryByRange(ctx, &orderdto.OrderRangeSummaryReq{
			ProjectID: pid, From: today, To: today,
		}); err != nil {
			snap.fail("order", pid, err)
		} else {
			snap.KPI.TodayOrders += res.OrderCount
			snap.KPI.TodaySalesCents += res.NetSales
		}

		if res, err := h.overviewOrders.StatusCounts(ctx, &orderdto.OrderStatusCountsReq{ProjectID: pid}); err != nil {
			snap.fail("order", pid, err)
		} else {
			snap.KPI.ShipPendingCount += res.ShipPendingCount
			snap.KPI.PendingCount += res.PendingCount
		}

		if res, err := h.overviewOrders.DailySeries(ctx, &orderdto.OrderDailySeriesReq{
			ProjectID: pid, From: from, To: today,
		}); err != nil {
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
			ProjectID: pid, From: from, To: today, Limit: overviewTopLimit,
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
	snap.Trend = buildTrend(byDay)
	snap.Top = buildTop(products)
}

// collectArticleViews 统计**当天**发生在文章页上的浏览量。
//
// 两步而非一步：analytics 只认 path，判断「这个路径是不是文章页」要靠 page 模块
// （表隔离：analytics 读不到 pages）。查不到类型的路径按**非文章页**处理 ——
// 猜一个默认值会让已下线的文章页继续被算进来，而两边都不会报错。
func (h *Handle) collectArticleViews(ctx context.Context, projectIDs []string, today string, snap *overviewSnapshot) {
	if h.overviewAnalytics == nil || h.overviewPageKinds == nil {
		return
	}
	snap.AnalyticsReady = true
	for _, pid := range projectIDs {
		res, err := h.overviewAnalytics.Summary(ctx, &analyticsdto.SummaryReq{
			ProjectID: pid, From: today, To: today, PathLimit: overviewPathLimit,
		})
		if err != nil {
			snap.fail("analytics", pid, err)
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
		}
	}
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

// buildTrend 把「天 → 点」的映射整理成升序序列并算好柱高。
func buildTrend(byDay map[string]*overviewTrendPoint) []overviewTrendPoint {
	points := make([]overviewTrendPoint, 0, len(byDay))
	for _, p := range byDay {
		points = append(points, *p)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Day < points[j].Day })

	var maxSales int64
	for _, p := range points {
		if p.NetSales > maxSales {
			maxSales = p.NetSales
		}
	}
	for i := range points {
		points[i].SalesLabel = moneyLabel(points[i].NetSales)
		if len(points[i].Day) >= 10 {
			points[i].DayLabel = points[i].Day[5:10]
		} else {
			points[i].DayLabel = points[i].Day
		}
		if maxSales <= 0 {
			continue
		}
		// 柱高按净销售额归一。最小 4% 是「有单但很少」的那天不至于看不见 ——
		// 归一到 0 会和「一单都没有」长得一样，而这两件事对运营是不同的信息。
		pct := int(points[i].NetSales * 100 / maxSales)
		if points[i].NetSales > 0 && pct < 4 {
			pct = 4
		}
		points[i].HeightPct = pct
	}
	return points
}

// buildTop 跨工程合并榜单：按销量降序（并列看金额与名字），重排名次后取前 N。
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

// moneyLabel 分 → 展示串（默认货币 + 两位小数 + 千分位）。
//
// 与 order/cart 的两处换算不是同一件事：那边一个给「X 元」拼句用（裸数字）、
// 一个给购物车行用（¥ 前缀无千分位），这里是概览 KPI 的展示形态。
// 跨工程求和必须在本层重算 —— 上游给的是**单个工程**的 label，直接相加是错的。
func moneyLabel(cents int64) string {
	currency := strings.TrimSpace(i18n.GetDefaultCurrency())
	raw := formatCents(cents)
	if currency == "" {
		return raw
	}
	return currency + " " + raw
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

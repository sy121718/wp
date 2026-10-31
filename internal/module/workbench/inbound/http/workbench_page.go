package workbenchhttp

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	analyticsdto "go_wp/internal/module/analytics/dto"
	orderdto "go_wp/internal/module/order/dto"
	pageenums "go_wp/internal/module/page/enums"
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

const (
	overviewTopLimit = 10

	overviewPageTopLimit = 10

	overviewPathLimit = 200
)

type OverviewOrderPort interface {
	SummaryByRange(ctx context.Context, req *orderdto.OrderRangeSummaryReq) (res *orderdto.OrderRangeSummaryResp, err error)
	DailySeries(ctx context.Context, req *orderdto.OrderDailySeriesReq) (res *orderdto.OrderDailySeriesResp, err error)

	HourlySeries(ctx context.Context, req *orderdto.OrderDailySeriesReq) (res *orderdto.OrderDailySeriesResp, err error)
	TopProducts(ctx context.Context, req *orderdto.OrderTopProductsReq) (res *orderdto.OrderTopProductsResp, err error)
	StatusCounts(ctx context.Context, req *orderdto.OrderStatusCountsReq) (res *orderdto.OrderStatusCountsResp, err error)
	SoldQuantityByRange(ctx context.Context, req *orderdto.OrderSoldQuantityReq) (res *orderdto.OrderSoldQuantityResp, err error)

	CustomerGrowthByRange(ctx context.Context, req *orderdto.CustomerGrowthReq) (res *orderdto.CustomerGrowthResp, err error)
}

type OverviewAnalyticsPort interface {
	Summary(ctx context.Context, req *analyticsdto.SummaryReq) (res *analyticsdto.SummaryResp, err error)
}

type OverviewPageKindPort interface {
	KindsOfPaths(ctx context.Context, projectID string, paths []string) (map[string]string, error)
}

type OverviewCurrencyPort interface {
	ListDictOptions(ctx context.Context, dictType string) ([]sysconfigdto.DictOption, error)
}

type overviewKPI struct {
	RangeOrders     int64
	RangeSalesCents int64
	RangeSalesLabel string

	RangeItems int64

	PageViews int64

	ArticleViews int64

	NewCustomers     int64
	ShipPendingCount int64
	PendingCount     int64
}

type overviewTrendPoint struct {
	Day string

	DayLabel   string
	Orders     int64
	NetSales   int64
	SalesLabel string

	Views int64

	SalesHeightPct int
	ViewsHeightPct int

	X        int
	BarWidth int

	ShowLabel bool

	LabelX      int
	LabelAnchor string
}

type overviewTopProduct struct {
	Rank        int
	ProductName string
	SKU         string
	Quantity    int64
	AmountLabel string
}

type overviewTopPage struct {
	Rank  int
	Path  string
	Kind  string
	Views int64

	KindKey string
}

type overviewSnapshot struct {
	OrdersReady    bool
	AnalyticsReady bool

	Range overviewRange

	Presets []rangePreset

	GranularityPresets []granularityPreset
	KPI                overviewKPI
	Trend              []overviewTrendPoint
	Top                []overviewTopProduct

	TopPages []overviewTopPage

	PortsReady bool

	FailedBlocks []string
}

func (h *Handle) Dashboard(c *gin.Context) {

	if h.projects == nil || h.pages == nil {
		pageview.RenderTemplate(c, DashboardPageTemplate, &DashboardPageResp{
			Title: workbenchenums.MsgDashboardTitle,
			Menu:  "dashboard",
		})
		return
	}

	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {

		logger.Scene("workbench").Error(err, "概览数据读取失败")
		pageview.RenderTemplate(c, DashboardPageTemplate, &DashboardPageResp{
			Title: workbenchenums.MsgDashboardTitle,
			Menu:  "dashboard",
			Err:   response.InternalText(c),
		})
		return
	}

	pageview.RenderTemplate(c, DashboardPageTemplate, &DashboardPageResp{
		Title: workbenchenums.MsgDashboardTitle,
		Menu:  "dashboard",

		Overview: withPresets(c, h.collectOverview(ctx, projectIDs(projects), parseOverviewRange(c, time.Now().UTC()))),
	})
}

func projectIDs(projects []projectcontract.ProjectResp) []string {
	ids := make([]string, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	return ids
}

func (h *Handle) SetOverviewPorts(orders OverviewOrderPort, analytics OverviewAnalyticsPort,
	pageKinds OverviewPageKindPort, currencies OverviewCurrencyPort) {
	h.overviewOrders = orders
	h.overviewAnalytics = analytics
	h.overviewPageKinds = pageKinds
	h.overviewCurrencies = currencies
}

func (h *Handle) currencySymbol(ctx context.Context) string {
	code := strings.TrimSpace(i18n.GetDefaultCurrency())
	if h.overviewCurrencies == nil || code == "" {
		return code
	}
	opts, err := h.overviewCurrencies.ListDictOptions(ctx, "currency")
	if err != nil {

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

func (h *Handle) collectOverview(ctx context.Context, projectIDs []string, rng overviewRange) overviewSnapshot {
	snap := overviewSnapshot{Range: rng}
	byDay := h.collectOrderOverview(ctx, projectIDs, rng, &snap)
	viewsByDay := make(map[string]int64)
	viewsByPath := make(map[string]int64)
	h.collectViews(ctx, projectIDs, rng, &snap, viewsByDay, viewsByPath)

	snap.Trend = buildTrend(byDay, viewsByDay, rng.Granularity, h.currencySymbol(ctx))
	snap.KPI.RangeSalesLabel = moneyLabel(snap.KPI.RangeSalesCents, h.currencySymbol(ctx))
	snap.PortsReady = snap.OrdersReady && snap.AnalyticsReady
	return snap
}

func (h *Handle) collectOrderOverview(ctx context.Context, projectIDs []string, rng overviewRange, snap *overviewSnapshot) map[string]*overviewTrendPoint {
	if h.overviewOrders == nil {
		return nil
	}
	snap.OrdersReady = true
	byDay := make(map[string]*overviewTrendPoint, rng.Days)
	var products []overviewTopProduct

	for _, pid := range projectIDs {

		if res, err := h.overviewOrders.SummaryByRange(ctx, &orderdto.OrderRangeSummaryReq{
			ProjectID: pid, From: rng.From, To: rng.To,
		}); err != nil {
			snap.fail("order", pid, err)
		} else {
			snap.KPI.RangeOrders += res.OrderCount
			snap.KPI.RangeSalesCents += res.NetSales
		}

		if res, err := h.overviewOrders.SoldQuantityByRange(ctx, &orderdto.OrderSoldQuantityReq{
			ProjectID: pid, From: rng.From, To: rng.To,
		}); err != nil {
			snap.fail("order", pid, err)
		} else {
			snap.KPI.RangeItems += res.Quantity
		}

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

func (h *Handle) trendSeries(ctx context.Context, projectID string, rng overviewRange) (*orderdto.OrderDailySeriesResp, error) {
	req := &orderdto.OrderDailySeriesReq{ProjectID: projectID, From: rng.From, To: rng.To}
	if rng.Granularity == trendGranularityHour {
		return h.overviewOrders.HourlySeries(ctx, req)
	}
	return h.overviewOrders.DailySeries(ctx, req)
}

func (h *Handle) collectViews(ctx context.Context, projectIDs []string, rng overviewRange, snap *overviewSnapshot, viewsByDay map[string]int64, viewsByPath map[string]int64) {
	if h.overviewAnalytics == nil {
		return
	}
	snap.AnalyticsReady = true

	kindOfPath := make(map[string]string)
	for _, pid := range projectIDs {
		res, err := h.overviewAnalytics.Summary(ctx, &analyticsdto.SummaryReq{
			ProjectID: pid, From: rng.From, To: rng.To, PathLimit: overviewPathLimit,

			Granularity: rng.Granularity,
		})
		if err != nil {
			snap.fail("analytics", pid, err)
			continue
		}
		snap.KPI.PageViews += res.Total

		for _, d := range res.Daily {
			viewsByDay[d.Day] += d.Views
		}

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

			if kindOfPath[p.Path] == "" && kinds[p.Path] != "" {
				kindOfPath[p.Path] = kinds[p.Path]
			}
		}
	}
	snap.TopPages = buildTopPages(viewsByPath, kindOfPath)
}

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

const (
	trendChartWidth = 900

	trendBarGap = 4

	trendBarMaxWidth = 88

	trendLabelMax = 12
)

func buildTrend(byDay map[string]*overviewTrendPoint, viewsByDay map[string]int64, granularity string, symbol string) []overviewTrendPoint {
	buckets := make(map[string]*overviewTrendPoint, len(byDay)+len(viewsByDay))
	bucketKey := func(day string) string { return trendBucketKey(day, granularity) }

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
	slices.SortFunc(points, func(a, b overviewTrendPoint) int { return cmp.Compare(a.Day, b.Day) })

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

		barWidth = 2
	}

	if barWidth > trendBarMaxWidth {
		barWidth = trendBarMaxWidth
	}
	labelEvery := (n + trendLabelMax - 1) / trendLabelMax

	lastThinned := ((n - 1) / labelEvery) * labelEvery

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

const (
	trendAnchorStart  = "start"
	trendAnchorMiddle = "middle"
	trendAnchorEnd    = "end"
)

func weekStartOf(day string) string {
	t, err := time.Parse(utils.LayoutDay, day)
	if err != nil {
		return day
	}
	return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7)).Format(utils.LayoutDay)
}

func buildTopPages(viewsByPath map[string]int64, kindOfPath map[string]string) []overviewTopPage {
	items := make([]overviewTopPage, 0, len(viewsByPath))
	for path, views := range viewsByPath {
		kind := kindOfPath[path]
		items = append(items, overviewTopPage{
			Path: path, Views: views, Kind: kind, KindKey: overviewPageKindKey(kind),
		})
	}
	slices.SortFunc(items, func(a, b overviewTopPage) int {
		return cmp.Or(cmp.Compare(b.Views, a.Views), cmp.Compare(a.Path, b.Path))
	})
	if len(items) > overviewPageTopLimit {
		items = items[:overviewPageTopLimit]
	}
	for i := range items {
		items[i].Rank = i + 1
	}
	return items
}

func overviewPageKindKey(kind string) string {
	switch kind {
	case pageenums.PageKindHome:
		return workbenchenums.DashboardPageKindHome
	case pageenums.PageKindPage:
		return workbenchenums.DashboardPageKindPage
	case pageenums.PageKindArticle:
		return workbenchenums.DashboardPageKindArticle
	case pageenums.PageKindTag:
		return workbenchenums.DashboardPageKindTag
	case pageenums.PageKindArchive:
		return workbenchenums.DashboardPageKindArchive
	case pageenums.PageKindSearch:
		return workbenchenums.DashboardPageKindSearch

	case pageenums.PageKindNotFound:
		return workbenchenums.DashboardPageKindNotFound
	default:
		return ""
	}
}

func buildTop(products []overviewTopProduct) []overviewTopProduct {
	if len(products) == 0 {
		return nil
	}
	slices.SortStableFunc(products, func(a, b overviewTopProduct) int {
		return cmp.Or(
			cmp.Compare(b.Quantity, a.Quantity),
			cmp.Compare(a.ProductName, b.ProductName),
			cmp.Compare(a.SKU, b.SKU),
		)
	})
	if len(products) > overviewTopLimit {
		products = products[:overviewTopLimit]
	}
	for i := range products {
		products[i].Rank = i + 1
	}
	return products
}

func moneyLabel(cents int64, symbol string) string {
	raw := formatCents(cents)
	sym := strings.TrimSpace(symbol)
	if sym == "" {
		return raw
	}

	return sym + raw
}

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

const dashboardPath = "/admin"

const (
	rangeToday     = "today"
	rangeYesterday = "yesterday"
	rangeWeek      = "week"
	rangeMonth     = "month"
	rangeYear      = "year"
	rangeCustom    = "custom"
)

const rangeDefault = rangeWeek

const rangeMaxDays = 366

const trendDailyMaxDays = 31

const trendHourlyMaxDays = 2

const (
	trendGranularityHour  = "hour"
	trendGranularityDay   = "day"
	trendGranularityWeek  = "week"
	trendGranularityMonth = "month"
)

var trendGranularityKeys = []string{
	trendGranularityHour,
	trendGranularityDay,
	trendGranularityWeek,
	trendGranularityMonth,
}

func granularityLabelKey(g string) string {
	return workbenchenums.DashboardTrendGranularityPrefix + g
}

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

type overviewRange struct {
	Key string

	From string
	To   string

	Days int

	Granularity string

	GranularityPresets []granularityPreset

	GranularityTitleKey string
	GranularityTitle    string

	Clamped bool
}

type rangePreset struct {
	Key string

	LabelKey string
	Label    string

	URL    string
	Active bool
}

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

		r = defaultRange(today)
	}
	return applyGranularity(r, c.Query("granularity"))
}

func granularityTitleKey(g string) string {
	return workbenchenums.DashboardTrendTitlePrefix + g
}

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

func isTrendGranularity(g string) bool {
	for _, k := range trendGranularityKeys {
		if k == g {
			return true
		}
	}
	return false
}

func defaultRange(today time.Time) overviewRange {
	offset := (int(today.Weekday()) + 6) % 7
	d := func(t time.Time) string { return t.Format(utils.LayoutDay) }
	return newRange(rangeDefault, d(today.AddDate(0, 0, -offset)), d(today), false)
}

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

type granularityPreset struct {
	Key      string
	LabelKey string
	Label    string
	URL      string
	Active   bool
}

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

func rangePresets(c *gin.Context, active overviewRange) []rangePreset {
	keys := []string{rangeToday, rangeYesterday, rangeWeek, rangeMonth, rangeYear, rangeCustom}
	out := make([]rangePreset, 0, len(keys))
	for _, k := range keys {
		out = append(out, rangePreset{
			Key:      k,
			LabelKey: workbenchenums.DashboardRangePrefix + k,
			Label:    rangePresetLabel(k),
			URL:      rangePresetURL(c, k, active.From, active.To),
			Active:   active.Key == k,
		})
	}
	return out
}

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

func withPresets(c *gin.Context, snap overviewSnapshot) overviewSnapshot {
	snap.Presets = rangePresets(c, snap.Range)
	snap.GranularityPresets = granularityPresets(c, snap.Range)
	return snap
}

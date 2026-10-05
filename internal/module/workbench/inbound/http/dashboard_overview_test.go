package workbenchhttp

// dashboard_overview_test.go — 概览页跨模块取数的单元判据（stub 端口，不连库）。
//
// 这里钉的是「拼装」这一层：多工程累加、趋势按天合并与柱高归一、榜单合并与截断、
// 文章浏览只算文章页、以及**单块失败不拖垮整页**。SQL 与口径的正确性由 order /
// page / analytics 各自的测试负责，这里只保证它们被正确地问、正确地拼。

import (
	"context"
	"errors"
	"strings"
	"testing"

	analyticsdto "go_wp/internal/module/analytics/dto"
	orderdto "go_wp/internal/module/order/dto"
	pageenums "go_wp/internal/module/page/enums"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
)

type stubOrderPort struct {
	summary  map[string]*orderdto.OrderRangeSummaryResp
	status   map[string]*orderdto.OrderStatusCountsResp
	daily    map[string]*orderdto.OrderDailySeriesResp
	top      map[string]*orderdto.OrderTopProductsResp
	items    map[string]*orderdto.OrderSoldQuantityResp
	growth   map[string]*orderdto.CustomerGrowthResp
	failWith error
	calls    int
	// growthReq 记下最后一次收到的区间：概览页与客户概览页必须落在同一个窗口。
	growthReq *orderdto.CustomerGrowthReq
}

// CustomerGrowthByRange 新客数（与客户概览页同一个聚合、同一个区间）。
func (s *stubOrderPort) CustomerGrowthByRange(_ context.Context, req *orderdto.CustomerGrowthReq) (*orderdto.CustomerGrowthResp, error) {
	s.growthReq = req
	if s.failWith != nil {
		return nil, s.failWith
	}
	if res, ok := s.growth[req.ProjectID]; ok {
		return res, nil
	}
	return &orderdto.CustomerGrowthResp{ProjectID: req.ProjectID}, nil
}

func (s *stubOrderPort) SummaryByRange(_ context.Context, req *orderdto.OrderRangeSummaryReq) (*orderdto.OrderRangeSummaryResp, error) {
	s.calls++
	if s.failWith != nil {
		return nil, s.failWith
	}
	if res, ok := s.summary[req.ProjectID]; ok {
		return res, nil
	}
	return &orderdto.OrderRangeSummaryResp{ProjectID: req.ProjectID}, nil
}

func (s *stubOrderPort) StatusCounts(_ context.Context, req *orderdto.OrderStatusCountsReq) (*orderdto.OrderStatusCountsResp, error) {
	if s.failWith != nil {
		return nil, s.failWith
	}
	if res, ok := s.status[req.ProjectID]; ok {
		return res, nil
	}
	return &orderdto.OrderStatusCountsResp{ProjectID: req.ProjectID}, nil
}

func (s *stubOrderPort) DailySeries(_ context.Context, req *orderdto.OrderDailySeriesReq) (*orderdto.OrderDailySeriesResp, error) {
	if s.failWith != nil {
		return nil, s.failWith
	}
	if res, ok := s.daily[req.ProjectID]; ok {
		return res, nil
	}
	return &orderdto.OrderDailySeriesResp{ProjectID: req.ProjectID}, nil
}

func (s *stubOrderPort) TopProducts(_ context.Context, req *orderdto.OrderTopProductsReq) (*orderdto.OrderTopProductsResp, error) {
	if s.failWith != nil {
		return nil, s.failWith
	}
	if res, ok := s.top[req.ProjectID]; ok {
		return res, nil
	}
	return &orderdto.OrderTopProductsResp{ProjectID: req.ProjectID}, nil
}

// SoldQuantityByRange 按工程回商品件数（缺省 0，与其它 stub 同形）。
func (s *stubOrderPort) SoldQuantityByRange(_ context.Context, req *orderdto.OrderSoldQuantityReq) (*orderdto.OrderSoldQuantityResp, error) {
	if s.failWith != nil {
		return nil, s.failWith
	}
	if res, ok := s.items[req.ProjectID]; ok {
		return res, nil
	}
	return &orderdto.OrderSoldQuantityResp{ProjectID: req.ProjectID}, nil
}

type stubAnalyticsPort struct {
	byProject map[string]*analyticsdto.SummaryResp
	failWith  error
}

func (s *stubAnalyticsPort) Summary(_ context.Context, req *analyticsdto.SummaryReq) (*analyticsdto.SummaryResp, error) {
	if s.failWith != nil {
		return nil, s.failWith
	}
	if res, ok := s.byProject[req.ProjectID]; ok {
		return res, nil
	}
	return &analyticsdto.SummaryResp{ProjectID: req.ProjectID}, nil
}

type stubPageKindPort struct {
	byProject map[string]map[string]string
	failWith  error
}

func (s *stubPageKindPort) KindsOfPaths(_ context.Context, projectID string, _ []string) (map[string]string, error) {
	if s.failWith != nil {
		return nil, s.failWith
	}
	if res, ok := s.byProject[projectID]; ok {
		return res, nil
	}
	return map[string]string{}, nil
}

func TestCollectOverviewAggregatesAcrossProjects(t *testing.T) {
	orders := &stubOrderPort{
		summary: map[string]*orderdto.OrderRangeSummaryResp{
			"p1": {OrderCount: 3, NetSales: 10000, NetSalesLabel: "100.00"},
			"p2": {OrderCount: 4, NetSales: 20050, NetSalesLabel: "200.50"},
		},
		status: map[string]*orderdto.OrderStatusCountsResp{
			"p1": {ShipPendingCount: 2, PendingCount: 1},
			"p2": {ShipPendingCount: 3, PendingCount: 0},
		},
		daily: map[string]*orderdto.OrderDailySeriesResp{
			"p1": {Points: []orderdto.OrderDailyPointDTO{
				{Day: "2026-09-29", OrderCount: 1, NetSales: 1000},
				{Day: "2026-10-05", OrderCount: 2, NetSales: 9000},
			}},
			"p2": {Points: []orderdto.OrderDailyPointDTO{
				{Day: "2026-09-29", OrderCount: 2, NetSales: 500},
				{Day: "2026-10-05", OrderCount: 2, NetSales: 1000},
			}},
		},
		top: map[string]*orderdto.OrderTopProductsResp{
			"p1": {Items: []orderdto.OrderTopProductItemDTO{{ProductName: "A", SKU: "A-1", Quantity: 5, Amount: 5000, AmountLabel: "50.00"}}},
			"p2": {Items: []orderdto.OrderTopProductItemDTO{{ProductName: "B", SKU: "B-1", Quantity: 9, Amount: 9000, AmountLabel: "90.00"}}},
		},
		items: map[string]*orderdto.OrderSoldQuantityResp{
			"p1": {Quantity: 5},
			"p2": {Quantity: 9},
		},
	}
	// Total 是**全站** PV（含非文章页路径），Paths 只是前 N 条里文章页那部分。
	// Daily 是两张图的日期轴来源之一：10-04 那天**只有浏览没有订单**，
	// 用来钉「两侧都要落进同一根日期轴」（少了它，那天会被静默丢掉）。
	analytics := &stubAnalyticsPort{byProject: map[string]*analyticsdto.SummaryResp{
		"p1": {Total: 45, Daily: []analyticsdto.DailyCount{
			{Day: "2026-09-29", Views: 40},
			{Day: "2026-10-04", Views: 5},
		}, Paths: []analyticsdto.PathCount{
			{Path: "/blog/a", Views: 30},
			{Path: "/about", Views: 7},
		}},
		"p2": {Total: 60, Daily: []analyticsdto.DailyCount{
			{Day: "2026-10-05", Views: 60},
		}, Paths: []analyticsdto.PathCount{{Path: "/blog/b", Views: 12}}},
	}}
	kinds := &stubPageKindPort{byProject: map[string]map[string]string{
		"p1": {"/blog/a": "article", "/about": "page"},
		"p2": {"/blog/b": "article"},
	}}

	h := &Handle{}
	h.SetOverviewPorts(orders, analytics, kinds, nil)
	snap := h.collectOverview(context.Background(), []string{"p1", "p2"}, testRange())

	if !snap.PortsReady {
		t.Fatal("三个端口都已注入，PortsReady 应为 true")
	}
	if snap.KPI.RangeOrders != 7 {
		t.Errorf("区间订单应跨工程累加 = 7，实得 %d", snap.KPI.RangeOrders)
	}
	if snap.KPI.RangeSalesCents != 30050 {
		t.Errorf("区间销售额 = %d 分，期望 30050", snap.KPI.RangeSalesCents)
	}
	// 符号取自后台数据字典；本用例没注入字典，回落的形态是「货币代码紧贴数字」
	//（不带空格 —— 代码是口径标识，界面上不该出现「CNY 300.50」这种把标识当符号的写法）。
	// 注入字典后这里会是 ¥300.50，见 TestOverviewSalesLabelUsesDictSymbol。
	if snap.KPI.RangeSalesLabel != "CNY300.50" && snap.KPI.RangeSalesLabel != "300.50" {
		t.Errorf("销售额展示串 = %q（货币由站点默认货币决定）", snap.KPI.RangeSalesLabel)
	}
	if snap.KPI.ShipPendingCount != 5 || snap.KPI.PendingCount != 1 {
		t.Errorf("待发货/待付款 = %d/%d，期望 5/1", snap.KPI.ShipPendingCount, snap.KPI.PendingCount)
	}
	if snap.KPI.RangeItems != 14 {
		t.Errorf("商品销售总量应跨工程累加 = 14，实得 %d", snap.KPI.RangeItems)
	}
	if snap.KPI.PageViews != 105 {
		t.Errorf("页面浏览总量应取 analytics 的 Total 并跨工程累加（45+60），实得 %d", snap.KPI.PageViews)
	}
	if snap.KPI.ArticleViews != 42 {
		t.Errorf("其中文章页应只算 article 路径（30+12），实得 %d", snap.KPI.ArticleViews)
	}
	// 日期轴是**订单与浏览量的并集**：09-29 / 10-04 / 10-05（10-04 只有浏览）。
	if len(snap.Trend) != 3 {
		t.Fatalf("趋势点应合并成 3 天，实得 %d：%+v", len(snap.Trend), snap.Trend)
	}
	byDay := map[string]overviewTrendPoint{}
	for _, p := range snap.Trend {
		byDay[p.Day] = p
	}
	for i, want := range []string{"2026-09-29", "2026-10-04", "2026-10-05"} {
		if snap.Trend[i].Day != want {
			t.Errorf("第 %d 天应为 %s（按日期升序），实得 %s", i, want, snap.Trend[i].Day)
		}
	}
	if got := byDay["2026-09-29"]; got.Orders != 3 || got.Views != 40 {
		t.Errorf("09-29 应为 3 单 / 40 浏览，实得 %d / %d", got.Orders, got.Views)
	}
	// 只有浏览的那天：订单为 0，浏览照旧在轴上（少了它图会短一截且不报错）。
	if got := byDay["2026-10-04"]; got.Orders != 0 || got.Views != 5 {
		t.Errorf("10-04 应为 0 单 / 5 浏览（只有浏览没有订单的天不能被丢掉），实得 %d / %d", got.Orders, got.Views)
	}
	if got := byDay["2026-10-05"]; got.Orders != 4 || got.Views != 60 {
		t.Errorf("10-05 应为 4 单 / 60 浏览，实得 %d / %d", got.Orders, got.Views)
	}
	// 两套柱高各自归一，互不影响：销售额最高的是 10-05，浏览量最高的也是 10-05，
	// 所以拿 10-04（Sales=0 / Views=5）当判据 —— 它的浏览量柱高必须来自浏览量那一套。
	if got := byDay["2026-10-05"]; got.SalesHeightPct != 100 || got.ViewsHeightPct != 100 {
		t.Errorf("10-05 两套柱高都该是 100（各自的最大值），实得 %d / %d", got.SalesHeightPct, got.ViewsHeightPct)
	}
	if got := byDay["2026-10-04"]; got.SalesHeightPct != 0 || got.ViewsHeightPct == 0 {
		t.Errorf("10-04 销售额柱高应为 0、浏览量柱高应大于 0（两套归一化不能互相借用），实得 %d / %d",
			got.SalesHeightPct, got.ViewsHeightPct)
	}
	if got := byDay["2026-09-29"]; got.SalesHeightPct == 0 {
		t.Errorf("有单的那天销售额柱高不该是 0（与「一单都没有」长得一样），实得 %+v", got)
	}
	if len(snap.Top) != 2 || snap.Top[0].ProductName != "B" || snap.Top[0].Rank != 1 {
		t.Errorf("榜单应跨工程按销量合并并重排名次，实得 %+v", snap.Top)
	}
	// 页面排行：跨工程合并后按浏览量降序（/blog/a 30 > /blog/b 12 > /about 7）。
	if len(snap.TopPages) != 3 {
		t.Fatalf("页面排行应合并成 3 条，实得 %d：%+v", len(snap.TopPages), snap.TopPages)
	}
	wantPages := []struct {
		path  string
		views int64
		kind  string
	}{
		{"/blog/a", 30, "article"},
		{"/blog/b", 12, "article"},
		{"/about", 7, "page"},
	}
	for i, want := range wantPages {
		got := snap.TopPages[i]
		if got.Path != want.path || got.Views != want.views || got.Rank != i+1 {
			t.Errorf("第 %d 条应为 %s / %d 浏览，实得 %+v", i+1, want.path, want.views, got)
		}
		if got.Kind != want.kind {
			t.Errorf("%s 的类型应为 %s，实得 %q", want.path, want.kind, got.Kind)
		}
		if got.KindKey == "" {
			t.Errorf("%s 的类型已知，KindKey 不该为空（否则页面上不渲染标签）", want.path)
		}
	}
}

// TestOverviewPageKindKeyCoversEveryKnownKind 页面类型 → 词条 key 的映射不漏项。
//
// 用 pageenums.PageKinds() 当输入：page 模块新增一种类型时这条会红，
// 而不是「页面上那枚标签凭空消失」（认不出的类型回空串，展示层就不渲染它）。
func TestOverviewPageKindKeyCoversEveryKnownKind(t *testing.T) {
	for _, kind := range pageenums.PageKinds() {
		if got := overviewPageKindKey(kind); got == "" {
			t.Errorf("页面类型 %q 没有对应的词条 key —— 新增类型时忘了补 overviewPageKindKey", kind)
		}
	}
	// 认不出的类型回空串（展示层据此不渲染标签，而不是渲染一个空 badge）。
	if got := overviewPageKindKey("no-such-kind"); got != "" {
		t.Errorf("未知类型应回空串，实得 %q", got)
	}
}

func TestCollectOverviewKeepsOtherBlocksWhenOneFails(t *testing.T) {
	// 订单块整体失败：订单侧 KPI / 榜单为空（本例的 analytics 没给按天数据，
	// 所以趋势也为空 —— 趋势轴是两侧的并集，有浏览量的那天照旧会出现在轴上），
	// 且失败信息只记**模块名**（错误文本进日志、不进页面）。
	orders := &stubOrderPort{failWith: errors.New("db down: password=secret")}
	analytics := &stubAnalyticsPort{byProject: map[string]*analyticsdto.SummaryResp{
		"p1": {Paths: []analyticsdto.PathCount{{Path: "/blog/a", Views: 11}}},
	}}
	kinds := &stubPageKindPort{byProject: map[string]map[string]string{"p1": {"/blog/a": "article"}}}

	h := &Handle{}
	h.SetOverviewPorts(orders, analytics, kinds, nil)
	snap := h.collectOverview(context.Background(), []string{"p1"}, testRange())

	if snap.KPI.RangeOrders != 0 || len(snap.Trend) != 0 || len(snap.Top) != 0 {
		t.Errorf("订单块失败时应为空：%+v", snap.KPI)
	}
	if snap.KPI.ArticleViews != 11 {
		t.Errorf("订单块失败不该影响文章浏览，实得 %d", snap.KPI.ArticleViews)
	}
	if len(snap.FailedBlocks) != 1 || snap.FailedBlocks[0] != "order" {
		t.Errorf("失败块应记模块名并去重，实得 %+v", snap.FailedBlocks)
	}
}

func TestCollectOverviewWithoutPortsIsNotReady(t *testing.T) {
	h := &Handle{}
	snap := h.collectOverview(context.Background(), []string{"p1"}, testRange())
	if snap.PortsReady {
		t.Fatal("未注入任何端口时 PortsReady 应为 false（页面据此显示「暂不可用」而不是一片 0）")
	}
	if snap.Range.From == "" || snap.Range.To == "" {
		t.Error("窗口在任何情况下都应有效（模板要显示区间）")
	}
}

// TestBuildTopCapsAtLimit 榜单截断：造得比上限多，验证截到上限且名次重排。
//
// 造数用 overviewTopLimit+3 而不是写死的 8：上限是常量（口径「热销商品前十」），
// 写死条数会让这个用例在上限调整时以「实得 N」的形式红掉，而它想钉的其实是截断这件事。
func TestBuildTopCapsAtLimit(t *testing.T) {
	n := overviewTopLimit + 3
	in := make([]overviewTopProduct, 0, n)
	for i := 0; i < n; i++ {
		in = append(in, overviewTopProduct{ProductName: string(rune('A' + i)), Quantity: int64(n - i)})
	}
	out := buildTop(in)
	if len(out) != overviewTopLimit {
		t.Fatalf("榜单应截到 %d 条，实得 %d", overviewTopLimit, len(out))
	}
	for i := range out {
		if out[i].Rank != i+1 {
			t.Errorf("第 %d 行的名次应为 %d，实得 %d", i, i+1, out[i].Rank)
		}
	}
}

func TestFormatCents(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{0, "0.00"},
		{5, "0.05"},
		{100, "1.00"},
		{123456, "1,234.56"},
		{100000000, "1,000,000.00"},
		{-250, "-2.50"},
	}
	for _, tc := range cases {
		if got := formatCents(tc.cents); got != tc.want {
			t.Errorf("formatCents(%d) = %q，期望 %q", tc.cents, got, tc.want)
		}
	}
}

// testRange 用例用的固定区间：7 天窗口（与改动前的固定窗口同形，断言数字不必跟着变）。
func testRange() overviewRange {
	return newRange(rangeWeek, "2026-01-01", "2026-01-07", false)
}

// TestOverviewKPIIncludesNewCustomers 概览页第 6 张卡：区间新客。
//
// 数字必须与客户概览页同源（同一个方法、同一个区间）—— 所以这里除了断言被累加，
// 还断言**传下去的区间就是页面的区间**：传错窗口不会报错，只会让两页显示两个新客数。
func TestOverviewKPIIncludesNewCustomers(t *testing.T) {
	orders := &stubOrderPort{growth: map[string]*orderdto.CustomerGrowthResp{
		"p1": {ProjectID: "p1", NewCustomers: 4},
		"p2": {ProjectID: "p2", NewCustomers: 3},
	}}
	h := &Handle{}
	h.SetOverviewPorts(orders, nil, nil, nil)
	rng := testRange()
	snap := h.collectOverview(context.Background(), []string{"p1", "p2"}, rng)

	if snap.KPI.NewCustomers != 7 {
		t.Errorf("两个工程的新客应累加为 7，实得 %d", snap.KPI.NewCustomers)
	}
	if orders.growthReq == nil {
		t.Fatal("没有调用区间客户增长")
	}
	if orders.growthReq.From != rng.From || orders.growthReq.To != rng.To {
		t.Errorf("传下去的区间应与页面区间一致：got %s~%s, want %s~%s",
			orders.growthReq.From, orders.growthReq.To, rng.From, rng.To)
	}
}

// stubCurrencyPort 只给 currency 字典供数。
type stubCurrencyPort struct {
	opts []sysconfigdto.DictOption
	err  error
}

func (s *stubCurrencyPort) ListDictOptions(context.Context, string) ([]sysconfigdto.DictOption, error) {
	return s.opts, s.err
}

// 金额前缀用**字典里的符号**，不是货币代码。
//
// 判据：界面上写「CNY 300.50」是把口径标识当符号用 —— 人读的是 ¥。
// 而符号的唯一来源是后台那张字典表（运营可增删货币），代码里另建一份映射必然漂移，
// 漂移的表现是「后台加了港币、概览页仍显示三字母代码」，不报错也没人知道改哪。
func TestOverviewSalesLabelUsesDictSymbol(t *testing.T) {
	orders := &stubOrderPort{summary: map[string]*orderdto.OrderRangeSummaryResp{
		"p1": {OrderCount: 1, NetSales: 12345, NetSalesLabel: "123.45"},
	}}
	h := &Handle{}
	h.SetOverviewPorts(orders, nil, nil, &stubCurrencyPort{opts: []sysconfigdto.DictOption{
		{Code: "CNY", Label: "CNY ¥", Symbol: "¥"},
	}})
	snap := h.collectOverview(context.Background(), []string{"p1"}, testRange())
	if snap.KPI.RangeSalesLabel != "¥123.45" {
		t.Fatalf("销售额应用字典里的符号，实得 %q", snap.KPI.RangeSalesLabel)
	}
}

// 字典读不到时金额照常显示（只是没有符号）—— 不能因为查字典失败把整张卡打空。
func TestOverviewSalesLabelSurvivesDictFailure(t *testing.T) {
	orders := &stubOrderPort{summary: map[string]*orderdto.OrderRangeSummaryResp{
		"p1": {OrderCount: 1, NetSales: 12345, NetSalesLabel: "123.45"},
	}}
	h := &Handle{}
	h.SetOverviewPorts(orders, nil, nil, &stubCurrencyPort{err: errors.New("字典暂时读不到")})
	snap := h.collectOverview(context.Background(), []string{"p1"}, testRange())
	if snap.KPI.RangeSalesLabel == "" {
		t.Fatal("字典失败不该让销售额为空")
	}
	if !strings.Contains(snap.KPI.RangeSalesLabel, "123.45") {
		t.Errorf("金额本身要照常显示，实得 %q", snap.KPI.RangeSalesLabel)
	}
}

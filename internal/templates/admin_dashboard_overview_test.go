package templates

// admin_dashboard_overview_test.go — 概览页新增块（KPI / 趋势 / 热销）的渲染判据。
//
// 为什么要单独测：模板里那些块整体包在 `{{if .Overview.PortsReady}}` 里，
// 而既有的 dashboard 渲染测试只传 title/menu/PageTotal/RecentPages ——
// 它验证的是「没有概览数据时页面不炸」，新块在这条路径上根本不执行。
// 于是模板里的字段名写错、类名写错、SVG 属性算错都不会被任何测试抓到，
// 只在生产上表现为「概览页少一块」。
//
// 用本地结构体而不是 workbenchhttp 的未导出类型：Jet 的字段访问只认字段名，
// 结构体在两边同形即可；这也让本包不必依赖 workbench 的 inbound 包。

import (
	"strings"
	"testing"
)

type tmplOverviewKPI struct {
	TodayOrders      int64
	TodaySalesLabel  string
	ArticleViews     int64
	ShipPendingCount int64
	PendingCount     int64
}

type tmplTrendPoint struct {
	Day        string
	DayLabel   string
	Orders     int64
	SalesLabel string
	HeightPct  int
}

type tmplTopProduct struct {
	Rank        int
	ProductName string
	SKU         string
	Quantity    int64
	AmountLabel string
}

type tmplOverview struct {
	PortsReady bool
	Today      string
	From       string
	KPI        tmplOverviewKPI
	Trend      []tmplTrendPoint
	Top        []tmplTopProduct
}

func TestDashboardRendersOverviewBlocks(t *testing.T) {
	// 以 adminShellData() 为底：它给了 layout 需要的 lang / csrf_token 与 **t 函数**。
	// 少了 t，模板里的 .["t"](...) 会取到 nil（渲染不报错、文案全空）——
	// 那正是「断言只测到了数字、没测到标签」的成因。
	data := adminShellData()
	data["menu"] = "dashboard"
	data["ProjectCount"] = 1
	data["PageTotal"] = 42
	data["PagePublished"] = 40
	data["PageDraft"] = 2
	data["PageStale"] = 1
	data["RecentPages"] = []map[string]any{}
	data["Overview"] = tmplOverview{
		PortsReady: true, Today: "2026-10-05", From: "2026-09-29",
		KPI: tmplOverviewKPI{
			TodayOrders: 12, TodaySalesLabel: "CNY 1,234.50",
			ArticleViews: 88, ShipPendingCount: 3, PendingCount: 2,
		},
		Trend: []tmplTrendPoint{
			{Day: "2026-09-29", DayLabel: "09-29", Orders: 1, SalesLabel: "CNY 10.00", HeightPct: 0},
			{Day: "2026-10-05", DayLabel: "10-05", Orders: 12, SalesLabel: "CNY 1,234.50", HeightPct: 100},
		},
		Top: []tmplTopProduct{
			{Rank: 1, ProductName: "TEO 香水 50ml", SKU: "TEO-50-01", Quantity: 5, AmountLabel: "CNY 400.00"},
		},
	}
	out, err := render(t, newAdminTestSet(), "admin/dashboard", data)
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}

	for _, want := range []string{
		"今日订单", "今日销售额", "CNY 1,234.50", "文章浏览", "88", "待发货",
		"销售趋势（近 7 天）", "热销商品（近 7 天）", "TEO 香水 50ml", "TEO-50-01", "09-29",
		// 柱状图是内联 SVG，柱高走属性（不是 style）。
		"<svg", "trend-bar", "height=\"120\"",
		// 未接线提示在这条路径上不该出现。
	} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染结果应含 %q", want)
		}
	}
	if strings.Contains(out, "暂不可用") {
		t.Error("端口已就绪时不该显示「暂不可用」提示")
	}
}

func TestDashboardOverviewDegradesWhenPortsMissing(t *testing.T) {
	data := adminShellData()
	data["menu"] = "dashboard"
	data["ProjectCount"] = 1
	data["PageTotal"] = 0
	data["PagePublished"] = 0
	data["PageDraft"] = 0
	data["PageStale"] = 0
	data["RecentPages"] = []map[string]any{}
	data["Overview"] = tmplOverview{PortsReady: false}
	out, err := render(t, newAdminTestSet(), "admin/dashboard", data)
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	if !strings.Contains(out, "暂不可用") {
		t.Error("端口未接线时应显示「暂不可用」提示")
	}
	// 关键：不能渲染一片 0 —— 0 会被当成真实统计（「今天一单都没有」）。
	for _, bad := range []string{"今日订单", "今日销售额", "文章浏览", "待发货"} {
		if strings.Contains(out, bad) {
			t.Errorf("端口未接线时不该渲染 KPI 卡 %q（一片 0 会被当成真实统计）", bad)
		}
	}
}

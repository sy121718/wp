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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tmplOverviewKPI struct {
	RangeOrders      int64
	RangeSalesLabel  string
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
	X          int
	BarWidth   int
	ShowLabel  bool
}

type tmplTopProduct struct {
	Rank        int
	ProductName string
	SKU         string
	Quantity    int64
	AmountLabel string
}

type tmplRange struct {
	Key     string
	From    string
	To      string
	Days    int
	Weekly  bool
	Clamped bool
}

type tmplRangePreset struct {
	Key      string
	LabelKey string
	Label    string
	URL      string
	Active   bool
}

type tmplOverview struct {
	PortsReady bool
	Range      tmplRange
	Presets    []tmplRangePreset
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
		PortsReady: true,
		Range:      tmplRange{Key: "week", From: "2026-09-29", To: "2026-10-05", Days: 7},
		Presets: []tmplRangePreset{
			{Key: "week", LabelKey: "admin.dashboard.range.week", Label: "本周", URL: "/admin?range=week", Active: true},
			{Key: "month", LabelKey: "admin.dashboard.range.month", Label: "本月", URL: "/admin?range=month"},
		},
		KPI: tmplOverviewKPI{
			RangeOrders: 12, RangeSalesLabel: "CNY 1,234.50",
			ArticleViews: 88, ShipPendingCount: 3, PendingCount: 2,
		},
		Trend: []tmplTrendPoint{
			{Day: "2026-09-29", DayLabel: "09-29", Orders: 1, SalesLabel: "CNY 10.00", HeightPct: 0, X: 1, BarWidth: 68, ShowLabel: true},
			{Day: "2026-10-05", DayLabel: "10-05", Orders: 12, SalesLabel: "CNY 1,234.50", HeightPct: 100, X: 71, BarWidth: 68, ShowLabel: true},
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
		"订单数", "净销售额", "CNY 1,234.50", "文章浏览", "88", "待发货",
		"销售趋势（按天）", "热销商品（当前区间）", "TEO 香水 50ml", "TEO-50-01", "09-29",
		// 时间筛选条：预设按钮、选中态、自定义区间的日期框（口径必须可见）。
		"range-bar", "range-chip", `href="/admin?range=week"`, "本周",
		`<input type="hidden" name="range" value="custom">`,
		// 日期框带基座类（外观只有一个真源）+ 本页的尺寸类。
		`class="form-input range-date"`,
		// 分开断言而不是连成 `name="from" value="..."`：属性之间还夹着 aria-label，
		// 连写会把「属性顺序」也变成判据，而顺序不是这里要钉的东西。
		`name="from"`, `value="2026-09-29"`,
		// KPI 卡可点击跳订单页（带口径的链接）。
		"stat-card-link", `href="/admin/orders"`,
		// 柱状图是内联 SVG，柱高走属性（不是 style）。
		"<svg", "trend-bar", "height=\"120\"",
		// 未接线提示在这条路径上不该出现。
	} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染结果应含 %q", want)
		}
	}
	// 选中态只该落在当前区间那一个按钮上（两个预设，一个 active）。
	if n := strings.Count(out, "range-chip is-active"); n != 1 {
		t.Errorf("选中的胶囊应恰好 1 个，实得 %d", n)
	}
	if strings.Contains(out, "（区间已按上限截取）") {
		t.Error("未发生收敛时不该显示截取提示")
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

// TestAdminDashboardFormActionMatchesRoute 钉筛选条表单的 action。
//
// 依据：仪表盘注册在 workbench 的**根级前缀组**里 —— router.go 的
// SetupWorkbenchRoutes 中写的是 `g.GET("/admin", h.Dashboard)`，没有 /admin/dashboard 这条。
// 写错了的表现是「点应用什么都不发生」（404 页面被 htmx/浏览器吞掉），
// 而模板渲染、Go 编译、其余测试全都不报错。
//
// 服务端那一半（预设胶囊的链接）由 workbenchhttp 的 dashboardPath 常量给出，
// 本用例额外禁止全文出现 /admin/dashboard，保证两处不会各自漂移。
func TestAdminDashboardFormActionMatchesRoute(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("admin/dashboard.html"))
	if err != nil {
		t.Fatalf("读取模板失败：%v", err)
	}
	src0 := src
	if !strings.Contains(string(src0), `action="/admin"`) {
		t.Error(`筛选条的 form action 应为 "/admin"（仪表盘的真实路由）`)
	}
	// 带前引号匹配**属性值**：说明文字里可以（也应当）写出错误路径长什么样，
	// 那正是这条注释存在的意义；要拦的是真把它写进 action / href。
	if strings.Contains(string(src0), `"/admin/dashboard`) {
		t.Error(`模板里不该把 /admin/dashboard 写进属性值：那条路由不存在（点下去 404，页面不报错）`)
	}
}

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
	RangeItems       int64
	PageViews        int64
	ArticleViews     int64
	NewCustomers     int64
	ShipPendingCount int64
	PendingCount     int64
}

type tmplTrendPoint struct {
	Day            string
	DayLabel       string
	Orders         int64
	SalesLabel     string
	Views          int64
	SalesHeightPct int
	ViewsHeightPct int
	X              int
	BarWidth       int
	ShowLabel      bool
	// LabelX / LabelAnchor 标签自身的锚点与对齐（首尾靠边对齐，避免桶多时标签越出画布）。
	LabelX      int
	LabelAnchor string
}

type tmplTopProduct struct {
	Rank        int
	ProductName string
	SKU         string
	Quantity    int64
	AmountLabel string
}

type tmplRange struct {
	Key  string
	From string
	To   string
	Days int
	// Granularity 走服务端算好的标题（模板不再 if/else 四档，Jet 没有 switch）。
	Granularity         string
	GranularityTitleKey string
	GranularityTitle    string
	Clamped             bool
}

type tmplGranularityPreset struct {
	Key      string
	LabelKey string
	Label    string
	URL      string
	Active   bool
}

type tmplRangePreset struct {
	Key      string
	LabelKey string
	Label    string
	URL      string
	Active   bool
}

// tmplTopPage 页面排行的一行（KindKey 是词条 key，不是类型原名）。
type tmplTopPage struct {
	Rank    int
	Path    string
	Kind    string
	Views   int64
	KindKey string
}

type tmplOverview struct {
	PortsReady         bool
	Range              tmplRange
	Presets            []tmplRangePreset
	GranularityPresets []tmplGranularityPreset
	KPI                tmplOverviewKPI
	Trend              []tmplTrendPoint
	Top                []tmplTopProduct
	TopPages           []tmplTopPage
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
		Range: tmplRange{
			Key: "week", From: "2026-09-29", To: "2026-10-05", Days: 7,
			Granularity:         "day",
			GranularityTitleKey: "admin.dashboard.trend.title.day",
			GranularityTitle:    "趋势（按天）",
		},
		Presets: []tmplRangePreset{
			{Key: "week", LabelKey: "admin.dashboard.range.week", Label: "本周", URL: "/admin?range=week", Active: true},
			{Key: "month", LabelKey: "admin.dashboard.range.month", Label: "本月", URL: "/admin?range=month"},
		},
		GranularityPresets: []tmplGranularityPreset{
			{Key: "hour", LabelKey: "admin.dashboard.trend.granularity.hour", Label: "小时", URL: "/admin?range=week&granularity=hour"},
			{Key: "day", LabelKey: "admin.dashboard.trend.granularity.day", Label: "天", URL: "/admin?range=week&granularity=day", Active: true},
			{Key: "week", LabelKey: "admin.dashboard.trend.granularity.week", Label: "周", URL: "/admin?range=week&granularity=week"},
			{Key: "month", LabelKey: "admin.dashboard.trend.granularity.month", Label: "月", URL: "/admin?range=week&granularity=month"},
		},
		KPI: tmplOverviewKPI{
			RangeOrders: 12, RangeSalesLabel: "¥1,234.50",
			RangeItems: 23, PageViews: 456,
			ArticleViews: 88, NewCustomers: 7, ShipPendingCount: 3, PendingCount: 2,
		},
		Trend: []tmplTrendPoint{
			// 09-29：有单但金额为 0（金额口径与件数不同），浏览量那套柱高不为 0 ——
			// 两条数据放在一起才能证明两张图各用各的字段。
			{Day: "2026-09-29", DayLabel: "09-29", Orders: 1, SalesLabel: "CNY 0.00", Views: 40, SalesHeightPct: 4, ViewsHeightPct: 66, X: 1, BarWidth: 68, ShowLabel: true},
			{Day: "2026-10-05", DayLabel: "10-05", Orders: 12, SalesLabel: "¥1,234.50", Views: 60, SalesHeightPct: 100, ViewsHeightPct: 100, X: 71, BarWidth: 68, ShowLabel: true},
		},
		Top: []tmplTopProduct{
			{Rank: 1, ProductName: "TEO 香水 50ml", SKU: "TEO-50-01", Quantity: 5, AmountLabel: "CNY 400.00"},
		},
		TopPages: []tmplTopPage{
			{Rank: 1, Path: "/blog/teo-50", Kind: "article", Views: 40, KindKey: "admin.dashboard.pageKind.article"},
			{Rank: 2, Path: "/about", Views: 7},
		},
	}
	out, err := render(t, newAdminTestSet(), "admin/dashboard", data)
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}

	for _, want := range []string{
		// 金额用**货币符号**紧贴数字（¥1,234.50），不是货币代码加空格 ——
		// 「CNY 300.50」是把口径标识当符号用；符号来自后台字典的 symbol 列。
		"订单数", "销售额", "¥1,234.50",
		"页面浏览", "456", "全站路径；其中文章页：", "88", "待发货",
		// 六卡合并成三卡后新增的卡标题与卡内横排容器（订单数 / 销售额 / 待发货
		// 三项同属「已付款」口径，拆成三张卡等于用三张卡讲同一句话）。
		"销售数据", "stat-grid-three", "stat-inline-row",
		// AI 提问区：结构与悬浮球同构（同一组 data-ai-fab-* 钩子），
		// 多出来的 data-ai-fab-dock 是「提问后沉到页面底部」的开关。
		"dash-ai", "data-ai-fab-dock", "data-ai-fab-input", "问 AI（经营数据）",
		"趋势（按天）", "排行榜（当前区间）", "热销商品", "热门页面", "TEO 香水 50ml", "TEO-50-01", "09-29",
		// 粒度切换：四个档位都要在页面上（缺一档 = 那个粒度没有入口，而默认值掩盖不了它），
		// 且选中档带 is-active —— 没有它用户看不出自己在看哪个粒度。
		"gran-bar", "小时", ">天<", ">周<", ">月<",
		`class="gran-btn is-active"`,
		// 链接里的 & 会被 HTML 转义成 &amp;（模板安全转义，写死原文会测不到）。
		`range=week&amp;granularity=day`,
		// 标题走服务端算好的词条键（模板里没有 if/else 四档）。
		"granularity=hour", "granularity=week", "granularity=month",
		// 时间筛选条：换成了后台统一的时间筛选条（admin/partials/date_filter.html）——
		// 快捷区间下拉 + 两个原生 date + 应用。原先那一排预设胶囊（服务端拼 URL 的
		// .range-chip）已经下线，所以这里不再断言胶囊，改为钉住组件的三件东西：
		// 下拉、两个日期框的 name、以及 form 的 action。
		"date-filter", "date-filter-preset", "name=\"range\"",
		// 日期框带基座类（外观只有一个真源）+ 本页的尺寸类。
		`class="form-input date-filter-date"`,
		// 分开断言而不是连成 `name="from" value="..."`：属性之间还夹着 aria-label，
		// 连写会把「属性顺序」也变成判据，而顺序不是这里要钉的东西。
		`name="from"`, `value="2026-09-29"`, `name="to"`, `value="2026-10-05"`,
		`action="/admin"`,
		// KPI 卡可点击跳订单页（带口径的链接）。
		"stat-card-link", `href="/admin/orders"`,
		// 新客卡（区间口径）。链接把当前区间带给客户概览页 ——
		// 不带的话点进去是那页自己的默认档，两页会显示两段不同时间的新客数而都像对的。
		"新客户", "7", `href="/admin/customers/overview?range=week"`,
		// 图表两个 Tab：面板全部渲染在服务端，切换由 admin.js 的 data-tabs 接管。
		`data-tabs`, `role="tablist"`, "dash-tab-sales", "dash-tab-views",
		`id="dash-panel-sales"`, `id="dash-panel-views"`,
		"销售额与订单", "页面浏览", "排行维度", "热销商品", "热门页面",
		// 柱状图是内联 SVG，柱高走属性（不是 style）。
		"<svg", "trend-bar", "height=\"135\"",
		// 未接线提示在这条路径上不该出现。
	} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染结果应含 %q", want)
		}
	}
	// （原先这里还断言「选中的胶囊恰好 1 个」。筛选条改成统一组件后胶囊已下线，
	// 选中态改为由前端按当前区间回填下拉，不再有服务端渲染的选中态可断言。）
	if strings.Contains(out, "（区间已按上限截取）") {
		t.Error("未发生收敛时不该显示截取提示")
	}
	// 已下线的两块内容不许回潮：用户明确要求取消「商品销售总量」卡、
	// 删掉「站点内容」与「最近更新的页面」两个区块。
	for _, gone := range []string{"商品销售总量", "站点工程", "页面总数", "最近更新的页面"} {
		if strings.Contains(out, gone) {
			t.Errorf("已下线的区块 %q 又出现在概览页上", gone)
		}
	}
	// 两个面板：销售额那个默认可见，浏览量那个带 hidden（由 JS 按 aria-selected 切换）。
	if !strings.Contains(out, `id="dash-panel-views" role="tabpanel" aria-labelledby="dash-tab-views" hidden`) {
		t.Error("浏览量面板初始应带 hidden（选中态由 data-tabs 的那套 aria 属性表达）")
	}
	if strings.Contains(out, `id="dash-panel-sales" role="tabpanel" aria-labelledby="dash-tab-sales" hidden`) {
		t.Error("销售额面板是默认选中的那个，不该带 hidden")
	}
	// 排行榜那一组同理：商品榜默认可见、页面榜带 hidden。
	if !strings.Contains(out, `id="dash-panel-toppage" role="tabpanel" aria-labelledby="dash-tab-toppage" hidden`) {
		t.Error("页面榜面板初始应带 hidden")
	}
	if strings.Contains(out, `id="dash-panel-topprod" role="tabpanel" aria-labelledby="dash-tab-topprod" hidden`) {
		t.Error("热销商品面板是默认选中的那个，不该带 hidden")
	}
	// 页面排行渲染的是**路径**（analytics 只记 path）。
	//
	// 类型标签取词的**文案**这里验不了：测试里的 t 是 stub（没有词条表），
	// 它的取值链是「词条缺失 → 回退第二参」，所以页面上会出现 Kind 原文。
	// 「词条真的存在」由 group F 的门禁（模板取词必须在迁移里 seed）与
	// pageKind.* 那六条本身的 seed 覆盖；这里只验结构。
	if !strings.Contains(out, "/blog/teo-50") || !strings.Contains(out, "/about") {
		t.Error("页面排行应渲染路径")
	}
	if !strings.Contains(out, `class="badge badge-mute"`) {
		t.Error("KindKey 非空的行应渲染类型标签（空 Key 的行不渲染，见下一条）")
	}
	if strings.Contains(out, `class="badge badge-mute">`+"/about") {
		t.Error("KindKey 为空的行不该渲染类型标签")
	}
	// 两张图各用各的柱高字段：浏览量图里 09-29 的柱高来自 ViewsHeightPct（66 -> 89.1），
	// 而销售额图里同一天是 SalesHeightPct（4 -> 5.4）。两个高度都必须在页面上出现。
	// 系数是 135/100（画布高 180、基线 135），不是 12/10 —— 画布从 490×160 改成
	// 900×180 时同步换过，这里跟着改，别让断言停在上一个画布尺寸上。
	if !strings.Contains(out, `height="89.1"`) {
		t.Error("浏览量图应出现 ViewsHeightPct 折算出的柱高（66% -> 89.1px），说明它没借用销售额的归一化")
	}
	if !strings.Contains(out, `height="5.4"`) {
		t.Error("销售额图应出现 SalesHeightPct 折算出的柱高（4% -> 5.4px）")
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
//
// 断言源文件而不是渲染产物：form 的建筑材料现在在共用组件
// internal/templates/admin/partials/date_filter.html 里，本页只传 formAction，
// 而「传的值是不是 /admin」这件事在渲染产物上会被组件展开淹没（产物里到处是 action）。
func TestAdminDashboardFormActionMatchesRoute(t *testing.T) {
	src, err := os.ReadFile(filepath.FromSlash("admin/dashboard.html"))
	if err != nil {
		t.Fatalf("读取模板失败：%v", err)
	}
	src0 := src
	if !strings.Contains(string(src0), `"formAction", "/admin"`) {
		t.Error(`筛选条的 formAction 应为 "/admin"（仪表盘的真实路由）`)
	}
	// 带前引号匹配**属性值**：说明文字里可以（也应当）写出错误路径长什么样，
	// 那正是这条注释存在的意义；要拦的是真把它写进 action / href。
	if strings.Contains(string(src0), `"/admin/dashboard`) {
		t.Error(`模板里不该把 /admin/dashboard 写进属性值：那条路由不存在（点下去 404，页面不报错）`)
	}
}

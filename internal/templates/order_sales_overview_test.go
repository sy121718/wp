package templates

// order_sales_overview_test.go — 销售概览页（/admin/orders/overview）的渲染判据。
//
// 为什么要单独测：这一页是**新增的整页模板**，它的键名（Filter.* / Cards / Compare /
// Trend.*）与视图层 map 里的键是**字符串对齐**的 ——
// 模板里把 `.Filter.StatusLabel` 写成 `.Filter.Status`、把 `.Trend.HasSales` 写成
// `.HasMonthlyData`，Jet 不会报错（取到 nil）、页面也不 500，只在生产上表现为
// 「那一块是空的」；而布尔键缺了更糟，`if` 要求 bool、缺失键求值成 nil 会**中断整页渲染**
// （实测命中过一次，报 `there is no field or method … in map[string]interface {}`）。
//
// 用本地结构体而不是 orderhttp 的未导出类型：Jet 的字段访问只认名字，
// 两边同形即可；这也让本包不必依赖订单模块的 inbound 包。

import (
	"strings"
	"testing"
)

// tmplSalesCard 一张统计卡（与 orderhttp.salesCardView 同形）。
type tmplSalesCard struct {
	Label  string
	Value  string
	Note   string
	Accent string
}

// tmplSalesCompareRow 环比一行（与 orderhttp.salesCompareRowView 同形）。
type tmplSalesCompareRow struct {
	Label    string
	Prev     string
	Change   string
	Up       *bool
	UpIsGood bool
	NoBase   bool
}

// tmplSalesFilter 筛选条回显值（与 orderhttp.salesFilterView 同形）。
type tmplSalesFilter struct {
	Project     string
	From        string
	To          string
	Status      string
	StatusLabel string
	Monthly     int
}

// tmplSalesProject 工程下拉项（projectdto.ProjectResp 的最小同形）。
type tmplSalesProject struct {
	ID   string
	Name string
}

// 折线图的四个类型与 orderhttp 的同名类型逐字段对齐（Jet 只认名字）。
type tmplSalesTrendDot struct {
	X     int
	Y     int
	Title string
}

type tmplSalesTrendSeries struct {
	Label  string
	Color  int
	Points string
	Total  string
	Dots   []tmplSalesTrendDot
}

type tmplSalesTrendAxis struct {
	X      int
	Label  string
	Anchor string
}

type tmplSalesTrendChart struct {
	HasData   bool
	HasSales  bool
	Series    []tmplSalesTrendSeries
	Axis      []tmplSalesTrendAxis
	Peak      int64
	PeakLabel string
}

func boolPtr(b bool) *bool { return &b }

// salesOverviewTemplateData 一份「有数据」的整页数据。
//
// 键名逐一对齐 orderhttp.salesOverviewView 的返回值 —— 少一个键，模板对应块静默为空；
// 少一个**布尔**键则整页渲染中断。
func salesOverviewTemplateData() map[string]any {
	data := adminShellData()
	data["menu"] = "orders"
	data["title"] = "销售概览"
	data["Err"] = ""
	data["LoadFailed"] = false
	data["StatusQuick"] = []map[string]any{
		{"Value": "", "Label": "全部", "Active": true},
		{"Value": "paid", "Label": "已付款", "Active": false},
	}
	data["Projects"] = []tmplSalesProject{{ID: "p1", Name: "测试工程"}}
	data["SelectedProject"] = "p1"
	data["Filter"] = tmplSalesFilter{
		Project: "p1", From: "2026-10-01", To: "2026-10-05",
		Status: "", StatusLabel: "全部", Monthly: 6,
	}
	data["From"] = "2026-10-01"
	data["To"] = "2026-10-05"
	data["Status"] = ""
	data["Monthly"] = 6
	data["HasData"] = true
	// 只有四张卡：本页讲的是「卖了多少钱」，客户侧只留 ACV（金额口径）。
	// 人数类的卡（下单客户 / 新客户 / 回头客户）与件数类的（平均每单件数）
	// 已从卡面移除 —— 它们同名口径仍在环比表与趋势图里。
	data["Cards"] = []tmplSalesCard{
		{Label: "订单数", Value: "12", Note: "含没有商品明细的订单；共 23 行明细", Accent: "primary"},
		{Label: "销售额", Value: "¥1,234.50", Note: "商品行实付合计", Accent: "success"},
		{Label: "平均订单价值", Value: "¥102.88", Note: "销售额 ÷ 订单数（AOV）", Accent: "info"},
		{Label: "平均客户价值", Value: "¥137.17", Note: "销售额 ÷ 下单客户数（ACV）", Accent: "mute"},
	}
	data["Compare"] = []tmplSalesCompareRow{
		{Label: "订单数", Prev: "10", Change: "+20.0%", Up: boolPtr(true), UpIsGood: true},
		{Label: "销售额", Prev: "¥900.00", Change: "+37.2%", Up: boolPtr(true), UpIsGood: true},
		// 无基期：Change 空串 + NoBase，模板必须显示「上期无数据」而不是一个空箭头位。
		{Label: "下单客户", Prev: "0", Change: "", NoBase: true},
	}
	// 两条线、两个月，坐标是视图层算好的「x,y」串 —— 模板只贴字符串，不做算术。
	data["Trend"] = tmplSalesTrendChart{
		HasData: true, HasSales: true,
		Peak: 123450, PeakLabel: "¥1,234.50",
		Series: []tmplSalesTrendSeries{
			{
				Label: "新客", Color: 3, Points: "6,66 994,20", Total: "¥1,000.00",
				Dots: []tmplSalesTrendDot{
					{X: 6, Y: 66, Title: "2026-09｜新客 ¥400.00｜合计 ¥900.00"},
					{X: 994, Y: 20, Title: "2026-10｜新客 ¥600.00｜合计 ¥1,234.50"},
				},
			},
			{
				Label: "游客单", Color: 8, Points: "6,140 994,138", Total: "¥334.50",
				Dots: []tmplSalesTrendDot{
					{X: 6, Y: 140, Title: "2026-09｜游客单 ¥200.00｜合计 ¥900.00"},
					{X: 994, Y: 138, Title: "2026-10｜游客单 ¥134.50｜合计 ¥1,234.50"},
				},
			},
		},
		Axis: []tmplSalesTrendAxis{
			{X: 6, Label: "2026-09", Anchor: "start"},
			{X: 994, Label: "2026-10", Anchor: "end"},
		},
	}
	return data
}

// 回看窗口里一笔销售都没有时的空态：不给一块空画布。
//
// 判据是**负向的**：这一支必须同时满足「没有折线画布」与「有那句空态文案」。
// 只断言文案会出现时，把空态分支接错（例如条件写成 HasData）测试仍然全绿 ——
// 而 HasData 在补零过的回看窗口里几乎恒真，等于空态永远不显示。
func TestSalesOverviewMonthlyEmptyState(t *testing.T) {
	data := salesOverviewTemplateData()
	data["Trend"] = tmplSalesTrendChart{HasData: true, HasSales: false}

	out := renderSalesOverview(t, data)
	if strings.Contains(out, `class="chart-trend-svg"`) {
		t.Error("没有销售时不该画出折线画布（一片空白会被读成趋势平缓）")
	}
	if !strings.Contains(out, "趋势暂无可画的线") {
		t.Error("没有销售时缺少空态文案")
	}
}

// renderSalesOverview 渲染整页模板（渲染期错误即失败）。
// 复用本包既有的 renderAdminPage（它已把 t / lang / csrf 之外的变量集处理好）。
func renderSalesOverview(t *testing.T, data map[string]any) string {
	t.Helper()
	return renderAdminPage(t, "admin/order/sales_overview.html", data)
}

// 四张卡的标题与值都出现，且口径说明进的是 .help-pop 浮层（不占卡面）。
func TestSalesOverviewRendersCards(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	for _, want := range []string{
		"订单数", "销售额", "平均订单价值", "平均客户价值",
		"¥1,234.50", "¥102.88", "¥137.17",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("页面缺少 %q", want)
		}
	}
	// 口径说明必须落在 .help-pop 浮层里，卡面上不留一个字。
	//
	// 曾经用原生 `title` —— 位置与样式由浏览器决定，实测悬浮时会压住相邻卡片
	// （页面上「直接出现一段说明文字」）。仓内其它页面用的是 .help-pop 浮层
	// （admin.js 已处理 hover / 键盘聚焦 / Esc / 点外部关闭 / 贴边自动翻转）。
	if !strings.Contains(out, `<span class="help-pop" role="tooltip">销售额 ÷ 订单数（AOV）</span>`) {
		t.Error("AOV 的口径说明没有渲染进 .help-pop 浮层")
	}
	if strings.Contains(out, `title="销售额`) || strings.Contains(out, `title="含没有商品明细`) {
		t.Error("口径说明仍在用原生 title 悬浮（会盖住相邻卡片）")
	}
	if !strings.Contains(out, `class="help-btn"`) {
		t.Error("卡片标签旁缺少 ? 说明按钮（浮层就打不开了）")
	}
	if strings.Contains(out, `<p class="stat-note">`) {
		t.Error("卡面上出现了说明段落（口径应藏在浮层里）")
	}
}

// 卡片只有一排四张：人数类的三张与件数类的那张已移除，也不再有「销售 / 客户」分组标题。
//
// 判据是负向的：这些词在页面上**别处也出现**（环比表的「下单客户」、卡片的「销售额」），
// 所以不能用「页面里含不含这几个字」，只能钉**已删掉的形状**——
// 卡面值（"1.92"）与分组标题的类名。
func TestSalesOverviewCardsAreSingleRowWithoutGroupTitles(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	if strings.Contains(out, "1.92") {
		t.Error("「平均每单件数」卡还在（它已从卡面移除，同名口径不再显示）")
	}
	if strings.Contains(out, `class="sales-group-title"`) {
		t.Error("出现了分组标题（卡片合成一排四张后不该再有「销售 / 客户」分组）")
	}
	// 一圈卡片只该有一个 .sales-cards 网格。两排的写法会让它出现两次。
	if n := strings.Count(out, `class="stat-grid sales-cards"`); n != 1 {
		t.Errorf("卡片网格应恰好 1 个，实际 %d 个", n)
	}
	if n := strings.Count(out, `class="card stat-card sales-card`); n != 4 {
		t.Errorf("卡片应恰好 4 张，实际 %d 张", n)
	}
}

// 环比：有基期给变化率与方向类名；无基期给「上期无数据」且不染方向色。
func TestSalesOverviewRendersCompare(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	if !strings.Contains(out, "+20.0%") || !strings.Contains(out, "+37.2%") {
		t.Error("环比变化率没有渲染")
	}
	if !strings.Contains(out, `class="is-up"`) {
		t.Error("上升方向没有 is-up 类名")
	}
	if !strings.Contains(out, "上期无数据") {
		t.Error("无基期时没有给出「上期无数据」文案")
	}
	// 无基期那一行不能带方向色（Change 为空串时既不是 up 也不是 down）。
	if strings.Contains(out, `class="is-down"`) {
		t.Error("无基期时不该出现 is-down")
	}
}

// 趋势是折线（不是柱状）：每条线一个 polyline，颜色取 --chart-cN，横轴刻度带锚点。
//
// **判据里刻意钉住「没有柱状骨架」**：柱状那版的类名（.sales-stack / .sales-seg /
// .sales-col / .sales-bar-wrap）如果还留在 DOM 里，说明模板改了一半 ——
// 那种状态下页面能渲染、测试若只断言「折线在」也会全绿。
func TestSalesOverviewRendersTrendLines(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	if !strings.Contains(out, `class="chart-trend-svg"`) {
		t.Error("缺少折线画布")
	}
	for _, want := range []string{
		`class="chart-trend-line" points="6,66 994,20" style="stroke: var(--chart-c3)"`,
		`class="chart-trend-line" points="6,140 994,138" style="stroke: var(--chart-c8)"`,
		// 图例：颜色必须在这里解释一次，否则线读不出是谁。
		`class="chart-trend-legend-item"`, `新客`, `游客单`, "¥1,000.00", "¥334.50",
		// 横轴刻度（首尾贴边往内收，anchor 由服务端给）。
		`text-anchor="start"`, `text-anchor="end"`, "2026-09", "2026-10",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("折线图缺少 %q", want)
		}
	}
	for _, banned := range []string{
		`class="sales-stack`, `class="sales-seg`, `class="sales-col`, `class="sales-bar-wrap`,
	} {
		if strings.Contains(out, banned) {
			t.Errorf("模板里还留着柱状骨架 %q（折线改造只改了一半）", banned)
		}
	}
}

// 折线的每个数据点都要带读数：polyline 只能挂一个 title，整条线说不清「这个月多少」。
//
// 判据是**数量对齐**：点数 == 线数 × 月数。只断言「有 title」时，
// 少画一条线或漏一个月的点都会漏过去 —— 而那时图上就有个月份没有数字可读。
func TestSalesOverviewTrendDotsCarryReadings(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	if !strings.Contains(out, `class="chart-trend-point"`) {
		t.Fatal("折线点没有渲染（图上没有任何可读的数字）")
	}
	want := len(salesOverviewTemplateData()["Trend"].(tmplSalesTrendChart).Series) *
		len(salesOverviewTemplateData()["Trend"].(tmplSalesTrendChart).Axis)
	if n := strings.Count(out, `class="chart-trend-point"`); n != want {
		t.Errorf("折线点应 %d 个（线数 × 月数），实际 %d 个", want, n)
	}
	if !strings.Contains(out, "<title>2026-10｜新客 ¥600.00｜合计 ¥1,234.50</title>") {
		t.Error("折线点缺少悬停读数")
	}
}

// 筛选条：生效区间回显 + 状态快捷项 + 月数回填。
func TestSalesOverviewRendersFilter(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	for _, want := range []string{
		`value="2026-10-01"`, `value="2026-10-05"`,
		`name="monthly"`, `name="status"`, `name="project"`,
		"全部", "已付款",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("筛选条缺少 %q", want)
		}
	}
	// 选中的状态项要走 <option selected>（select 是原生控件，没有自定义的选中类名）。
	if !strings.Contains(out, `<option value="" selected>`) {
		t.Error("状态快捷项没有选中态")
	}
}

// 取数失败时：只给归口提示，**不渲染半张报表**（一半真一半 0 比空态危险）。
func TestSalesOverviewLoadFailedHidesReport(t *testing.T) {
	data := salesOverviewTemplateData()
	data["LoadFailed"] = true
	data["Err"] = "数据没能读出来，稍后重试。"
	data["Cards"] = []tmplSalesCard{}
	data["Compare"] = []tmplSalesCompareRow{}
	data["Trend"] = tmplSalesTrendChart{}
	data["HasData"] = false

	out := renderSalesOverview(t, data)

	if !strings.Contains(out, "数据没能读出来，稍后重试。") {
		t.Error("装载失败没有显示归口提示")
	}
	if strings.Contains(out, "¥1,234.50") {
		t.Error("装载失败时仍渲染了报表数字（半张报表比空态危险）")
	}
}

// 空态：区间内没有订单时给空态文案，不渲染报表。
func TestSalesOverviewEmptyState(t *testing.T) {
	data := salesOverviewTemplateData()
	data["HasData"] = false
	data["Cards"] = []tmplSalesCard{}
	data["Compare"] = []tmplSalesCompareRow{}
	data["Trend"] = tmplSalesTrendChart{}

	out := renderSalesOverview(t, data)

	if !strings.Contains(out, "这段时间没有计入消费的订单。") {
		t.Error("空态文案没有渲染")
	}
	if strings.Contains(out, "+20.0%") {
		t.Error("空态下仍渲染了环比（会给「这个区间有数据」的错觉）")
	}
}

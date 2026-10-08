package templates

// order_sales_overview_test.go — 销售概览页（/admin/orders/overview）的渲染判据。
//
// 为什么单独测：模板的数据键（Overview / FilterFrom / FilterTo / FilterStatus /
// Monthly / StatusQuickValues / OrderStatusLabel / LoadErr …）与控制器
// orderhttp.SalesOverviewPage 交给模板的 gin.H 是**字符串对齐**的 ——
// 键名写岔 Jet 不报错（取到 nil）、页面也不 500，只在生产上表现为「那一块是空的」；
// 而判空 / 布尔条件里的缺键更糟，会让**整页渲染中断**（HTTP 仍 200，后半截整块消失）。
//
// 这一页在 2026-10 做过一次架构改造：卡片的拼装与趋势图的几何计算从 Go 移进了模板，
// 所以测试数据不再是 orderhttp 的 salesCardView / salesTrendView 形状，而是
// **直接给 dto（orderdto.SalesOverviewResp）形状的 map** —— 模板按字段名读
// .Overview.OrderCount / .Overview.MonthlyPoints / .Overview.Compare 等。
//
// 为什么用 map 而不是直接给 orderdto.SalesOverviewResp：模板在金额前缀处读了
// .Overview.Currency，而该 dto 里没有这个字段 —— 直接给 struct 会报
// "can't use Currency as field name in struct type"，给 map 才能在字段表之外补上它。
// 这也让本包不必依赖订单模块的 dto 包（Jet 的字段访问只认名字，两边同形即可）。

import (
	"strings"
	"testing"
)

// salesOverviewDTO 一份「有数据」的 Overview（orderdto.SalesOverviewResp 形状）。
//
// 金额一律整数分（与 dto 同口径），展示串由模板的 money() 生成 —— 注意 money()
// **不做千分位分组**，所以断言里写的是 "¥1234.50" 而不是 "¥1,234.50"。
func salesOverviewDTO() map[string]any {
	return map[string]any{
		// 金额前缀（模板的 currencyPrefix 读它；dto 里没有这个字段，见文件头说明）。
		"Currency": "CNY",
		// ── 卡片口径 ──
		"OrderCount":       int64(12),
		"ItemRows":         int64(23),
		"Sales":            int64(123450), // ¥1234.50
		"AvgOrderValue":    int64(10288),  // ¥102.88
		"AvgCustomerValue": int64(13717),  // ¥137.17
		// 「平均每单件数」已从卡面移除：给一个值，断言它**不出现**在页面上。
		"AvgItemsPerOrder": 1.92,
		// ── 环比（紧邻的上一段等长区间）──
		"Compare": map[string]any{
			"From": "2026-09-01", "To": "2026-09-05",
			"OrderCount": int64(10), "Sales": int64(90000), "Customers": int64(0),
			"OrderCountChangePct": 20.0, // +20.0%
			"SalesChangePct":      37.2, // +37.2%
			// CustomersChangePct = 上一期为 0（无基期）→ 显示「上期无数据」而不是 0%。
			// 必须显式给**带类型的** nil 指针：Jet 不会把 map 缺键（无类型 nil）传给函数
			// （报 "argument for position 0 … is not a valid value"），
			// 而 dto 里这个字段本身就是 *float64（nil = 无基期），typed nil 才是同形数据。
			"CustomersChangePct": (*float64)(nil),
		},
		// ── 月度趋势（固定回看窗口，与卡片的筛选区间不同口径）──
		"Monthly": 6,
		"MonthlyPoints": []map[string]any{
			{"Month": "2026-09", "Sales": int64(90000), "NewSales": int64(40000), "ReturningSales": int64(30000), "GuestSales": int64(20000)},
			{"Month": "2026-10", "Sales": int64(123450), "NewSales": int64(60000), "ReturningSales": int64(50000), "GuestSales": int64(13450)},
		},
	}
}

// salesOverviewTemplateData 一份「有数据」的整页数据。
//
// 键名逐一对齐 orderhttp.SalesOverviewPage 交给模板的那一份 —— 少一个键，模板对应块
// 静默为空；少一个**判空 / 布尔**键则整页渲染中断。
func salesOverviewTemplateData() map[string]any {
	data := adminShellData()
	data["menu"] = "orders"
	data["title"] = "销售概览"
	data["Overview"] = salesOverviewDTO()
	// 生效区间与筛选回显（service 归一化后的值）。
	data["FilterFrom"] = "2026-10-01"
	data["FilterTo"] = "2026-10-05"
	data["FilterStatus"] = ""
	data["Monthly"] = 6
	// 状态快捷项取值 + 取词函数（空值项走「全部」自己的词条，其余走订单状态真源）。
	data["StatusQuickValues"] = []string{"", "paid", "shipped", "completed"}
	data["OrderStatusLabel"] = func(sv string) string {
		switch sv {
		case "paid":
			return "已付款"
		case "shipped":
			return "已发货"
		case "completed":
			return "已完成"
		}
		return sv
	}
	// 快捷项链接要保留的上下文查询串（模板里声明后复用）。
	data["ListQuery"] = "project=p1&from=2026-10-01&to=2026-10-05&monthly=6"
	// 装载失败原因（空串 = 正常）。
	data["LoadErr"] = ""
	// 工程下拉：给两个工程才会走 <select>（只有一个时渲染 hidden input）。
	data["Projects"] = []map[string]any{
		{"ID": "p1", "Name": "测试工程"},
		{"ID": "p2", "Name": "另一个工程"},
	}
	data["SelectedProject"] = "p1"
	return data
}

// renderSalesOverview 渲染整页模板（渲染期错误即失败）。
// 复用本包既有的 renderAdminPage（它已把 layout / sidebar 需要的键与全局函数准备好）。
func renderSalesOverview(t *testing.T, data map[string]any) string {
	t.Helper()
	return renderAdminPage(t, "admin/order/sales_overview.html", data)
}

// 四张卡的标题与值都出现，且口径说明进的是 .help-pop 浮层（不占卡面）。
func TestSalesOverviewRendersCards(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	for _, want := range []string{
		"订单数", "销售额", "平均订单价值", "平均客户价值",
		"¥1234.50", "¥102.88", "¥137.17",
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

// 环比：三行（订单数 / 销售额 / 下单客户），有基期给变化率与方向类名；
// 无基期给「上期无数据」且不染方向色。
func TestSalesOverviewRendersCompare(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	for _, want := range []string{
		"<td>订单数</td>", "<td>销售额</td>", "<td>下单客户</td>",
		"+20.0%", "+37.2%",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("环比表缺少 %q", want)
		}
	}
	if !strings.Contains(out, `class="is-up"`) {
		t.Error("上升方向没有 is-up 类名")
	}
	// 无基期（上一期为 0）：说「上期无数据」而不是 0% —— 0% 会让「上期一单没卖」
	// 看起来像「持平」。
	if !strings.Contains(out, "上期无数据") {
		t.Error("无基期时没有给出「上期无数据」文案")
	}
	// 无基期那一行不能带方向色（变化率为 nil 时既不是 up 也不是 down）。
	if strings.Contains(out, `class="is-down"`) {
		t.Error("无基期时不该出现 is-down")
	}
}

// 趋势是折线（不是柱状）：三条线（新客 / 回头客 / 游客）各一个 polyline，
// 颜色取 --chart-cN，图例解释颜色，横轴刻度带月份。
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
		`class="chart-trend-line"`,
		// 三条线各自的颜色（新客 / 回头客 / 游客）。
		"var(--chart-c3)", "var(--chart-c1)", "var(--chart-c8)",
		// 图例：颜色必须在这里解释一次，否则线读不出是谁。
		`class="chart-trend-legend-item"`, "新客", "回头客", "游客单",
		// 图例合计（各系列两个月之和）。
		"¥1000.00", "¥800.00", "¥334.50",
		// 横轴刻度（首尾贴边往内收，anchor 由模板给）。
		"2026-09", "2026-10",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("折线图缺少 %q", want)
		}
	}
	// 三条线 = 三个 polyline。
	if n := strings.Count(out, `class="chart-trend-line"`); n != 3 {
		t.Errorf("折线应有 3 条（新客 / 回头客 / 游客），实际 %d 条", n)
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
	data := salesOverviewTemplateData()
	out := renderSalesOverview(t, data)

	if !strings.Contains(out, `class="chart-trend-point"`) {
		t.Fatal("折线点没有渲染（图上没有任何可读的数字）")
	}
	// 点位数量 = 三条线 × 月份数（月份数来自 dto 的 MonthlyPoints）。
	months := len(data["Overview"].(map[string]any)["MonthlyPoints"].([]map[string]any))
	if want := 3 * months; strings.Count(out, `class="chart-trend-point"`) != want {
		t.Errorf("折线点应 %d 个（线数 × 月数），实际 %d 个",
			want, strings.Count(out, `class="chart-trend-point"`))
	}
	// 命中块的 title 里必须同时有月份与金额（这是本页唯一的读数入口）。
	if !strings.Contains(out, "<title>2026-10｜新客 ¥600.00｜合计 ¥1234.50</title>") {
		t.Error("折线点缺少悬停读数（月份 + 金额）")
	}
}

// 筛选条：工程下拉 + 生效日期区间回显 + 状态快捷项 + 月数回填。
func TestSalesOverviewRendersFilter(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	for _, want := range []string{
		// 日期区间的生效值（走 admin/partials/date_filter.html）。
		`value="2026-10-01"`, `value="2026-10-05"`,
		// 四个筛选项的字段名。
		`name="project"`, `name="status"`, `name="monthly"`,
		// 状态快捷项：空值走「全部」，其余走订单状态真源。
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
	data["LoadErr"] = "数据没能读出来，稍后重试。"
	// 控制器取数失败时不设 Overview（nil）—— 模板据 LoadErr 走失败分支。
	data["Overview"] = nil

	out := renderSalesOverview(t, data)

	if !strings.Contains(out, "数据没能读出来，稍后重试。") {
		t.Error("装载失败没有显示归口提示")
	}
	if strings.Contains(out, "¥1234.50") {
		t.Error("装载失败时仍渲染了报表数字（半张报表比空态危险）")
	}
	if strings.Contains(out, `class="stat-grid sales-cards"`) {
		t.Error("装载失败时仍渲染了卡片网格")
	}
}

// 空态：区间内没有订单时给空态文案，不渲染报表。
//
// 触发条件是模板里的 `!o || (o.OrderCount == 0 && o.Monthly == 0)` ——
// 给一份「区间内零订单」的 Overview（OrderCount / Monthly 都是 0）。
func TestSalesOverviewEmptyState(t *testing.T) {
	data := salesOverviewTemplateData()
	data["Overview"] = map[string]any{
		"Currency": "CNY", "OrderCount": int64(0), "Monthly": 0,
	}

	out := renderSalesOverview(t, data)

	if !strings.Contains(out, "这段时间没有计入消费的订单。") {
		t.Error("空态文案没有渲染")
	}
	if strings.Contains(out, "+20.0%") {
		t.Error("空态下仍渲染了环比（会给「这个区间有数据」的错觉）")
	}
	if strings.Contains(out, `class="stat-grid sales-cards"`) {
		t.Error("空态下仍渲染了卡片网格")
	}
}

// 月度趋势「一笔销售都没有」时给空态文案而不是一块空画布。
//
// 判据是**负向的**：这一支必须同时满足「没有折线画布」与「有那句空态文案」。
// 只断言文案会出现时，把空态分支接错测试仍然全绿 —— 而回看窗口里的月份是补零出来的，
// 「有数据」几乎恒真，等于空态永远不显示。
func TestSalesOverviewMonthlyEmptyState(t *testing.T) {
	data := salesOverviewTemplateData()
	dto := salesOverviewDTO()
	// 两个月都补零：窗口里没有任何销售额。
	dto["MonthlyPoints"] = []map[string]any{
		{"Month": "2026-09", "Sales": int64(0), "NewSales": int64(0), "ReturningSales": int64(0), "GuestSales": int64(0)},
		{"Month": "2026-10", "Sales": int64(0), "NewSales": int64(0), "ReturningSales": int64(0), "GuestSales": int64(0)},
	}
	data["Overview"] = dto

	out := renderSalesOverview(t, data)

	if strings.Contains(out, `class="chart-trend-svg"`) {
		t.Error("没有销售时不该画出折线画布（一片空白会被读成趋势平缓）")
	}
	if !strings.Contains(out, "趋势暂无可画的线") {
		t.Error("没有销售时缺少空态文案")
	}
}

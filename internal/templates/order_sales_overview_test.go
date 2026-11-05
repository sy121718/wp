package templates

import (
	"strings"
	"testing"
)

func salesOverviewDTO() map[string]any {
	return map[string]any{

		"Currency": "CNY",

		"OrderCount":       int64(12),
		"ItemRows":         int64(23),
		"Sales":            int64(123450),
		"AvgOrderValue":    int64(10288),
		"AvgCustomerValue": int64(13717),

		"Units":       int64(23),
		"GrossProfit": int64(45000),

		"CostIncomplete":     false,
		"CostIncompleteRows": int64(0),

		"AvgItemsPerOrder": 1.92,

		"Compare": map[string]any{
			"From": "2026-09-01", "To": "2026-09-05",
			"OrderCount": int64(10), "Sales": int64(90000), "Customers": int64(0),
			"OrderCountChangePct": 20.0,
			"SalesChangePct":      37.2,

			"CustomersChangePct": (*float64)(nil),
		},

		"Monthly": 6,
		"MonthlyPoints": []map[string]any{
			{"Month": "2026-09", "Sales": int64(90000), "NewSales": int64(40000), "ReturningSales": int64(30000), "GuestSales": int64(20000)},
			{"Month": "2026-10", "Sales": int64(123450), "NewSales": int64(60000), "ReturningSales": int64(50000), "GuestSales": int64(13450)},
		},
	}
}

func salesOverviewTemplateData() map[string]any {
	data := adminShellData()
	data["menu"] = "orders"
	data["title"] = "销售概览"
	data["Overview"] = salesOverviewDTO()

	data["FilterFrom"] = "2026-10-01"
	data["FilterTo"] = "2026-10-05"
	data["FilterStatus"] = ""
	data["Monthly"] = 6

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

	data["ListQuery"] = "project=p1&from=2026-10-01&to=2026-10-05&monthly=6"

	data["LoadErr"] = ""

	// Todo 必须是真实结构体字段名（Total/AlertCount/PendingShip）：adminui.ViewMap
	// 只转顶层字段，生产里 data["Todo"] 是 SalesTodoResp 值而不是 map，模板按
	// .Todo.Total 访问。这里写小写 map 等于模拟「view tag 在嵌套结构上生效」这个
	// 错误假设 —— 那正是老模板 500 的成因。不放真类型是因为 internal/templates
	// 不能 import internal/module/order/inbound/http（会绕回 internal/text 成环）。
	data["Todo"] = struct {
		AlertCount  int
		PendingShip int
		Total       int
	}{AlertCount: -1, PendingShip: -1, Total: 0}

	data["Projects"] = []map[string]any{
		{"ID": "p1", "Name": "测试工程"},
		{"ID": "p2", "Name": "另一个工程"},
	}
	data["SelectedProject"] = "p1"
	return data
}

func renderSalesOverview(t *testing.T, data map[string]any) string {
	t.Helper()
	return renderAdminPage(t, "admin/order/sales_overview.html", data)
}

func TestSalesOverviewRendersCards(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	for _, want := range []string{
		"订单数", "销售额", "总订购件数", "毛利",
		"¥1234.50", "¥450.00", "23",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("页面缺少 %q", want)
		}
	}

	if !strings.Contains(out, `<span class="help-pop" role="tooltip">`) ||
		!strings.Contains(out, "行实付 − 行成本 − 运费") {
		t.Error("毛利的说明没有渲染进 .help-pop 浮层")
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

func TestSalesOverviewCardsAreSingleRowWithoutGroupTitles(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	if strings.Contains(out, "1.92") {
		t.Error("「平均每单件数」卡还在（它已从卡面移除，同名口径不再显示）")
	}
	if strings.Contains(out, `class="sales-group-title"`) {
		t.Error("出现了分组标题（卡片合成一排四张后不该再有「销售 / 客户」分组）")
	}

	if n := strings.Count(out, `class="stat-grid sales-cards"`); n != 1 {
		t.Errorf("卡片网格应恰好 1 个，实际 %d 个", n)
	}
	if n := strings.Count(out, `class="card stat-card sales-card`); n != 4 {
		t.Errorf("卡片应恰好 4 张，实际 %d 张", n)
	}
}

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

	if !strings.Contains(out, "上期无数据") {
		t.Error("无基期时没有给出「上期无数据」文案")
	}

	if strings.Contains(out, `class="is-down"`) {
		t.Error("无基期时不该出现 is-down")
	}
}

func TestSalesOverviewRendersTrendLines(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	if !strings.Contains(out, `class="chart-trend-svg"`) {
		t.Error("缺少折线画布")
	}
	for _, want := range []string{
		`class="chart-trend-line"`,

		"var(--chart-c3)", "var(--chart-c1)", "var(--chart-c8)",

		`class="chart-trend-legend-item"`, "新客", "回头客", "游客单",

		"¥1000.00", "¥800.00", "¥334.50",

		"2026-09", "2026-10",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("折线图缺少 %q", want)
		}
	}

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

func TestSalesOverviewTrendDotsCarryReadings(t *testing.T) {
	data := salesOverviewTemplateData()
	out := renderSalesOverview(t, data)

	if !strings.Contains(out, `class="chart-trend-point"`) {
		t.Fatal("折线点没有渲染（图上没有任何可读的数字）")
	}

	months := len(data["Overview"].(map[string]any)["MonthlyPoints"].([]map[string]any))
	if want := 3 * months; strings.Count(out, `class="chart-trend-point"`) != want {
		t.Errorf("折线点应 %d 个（线数 × 月数），实际 %d 个",
			want, strings.Count(out, `class="chart-trend-point"`))
	}

	if !strings.Contains(out, "<title>2026-10｜新客 ¥600.00｜合计 ¥1234.50</title>") {
		t.Error("折线点缺少悬停读数（月份 + 金额）")
	}
}

func TestSalesOverviewRendersFilter(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	for _, want := range []string{

		`value="2026-10-01"`, `value="2026-10-05"`,

		`name="project"`, `name="status"`, `name="monthly"`,

		"全部", "已付款",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("筛选条缺少 %q", want)
		}
	}

	if !strings.Contains(out, `<option value="" selected>`) {
		t.Error("状态快捷项没有选中态")
	}
}

func TestSalesOverviewLoadFailedHidesReport(t *testing.T) {
	data := salesOverviewTemplateData()
	data["LoadErr"] = "数据没能读出来，稍后重试。"

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

func TestSalesOverviewMonthlyEmptyState(t *testing.T) {
	data := salesOverviewTemplateData()
	dto := salesOverviewDTO()

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

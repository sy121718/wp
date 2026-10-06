package templates

// order_sales_overview_test.go — 销售概览页（/admin/orders/overview）的渲染判据。
//
// 为什么要单独测：这一页是**新增的整页模板**，它的键名（Filter.* / Cards / Clients /
// Compare / MonthlyPoints）与视图层 map 里的键是**字符串对齐**的 ——
// 模板里把 `.Filter.StatusLabel` 写成 `.Filter.Status`、把 `.Zero` 写成 `.IsZero`，
// Jet 不会报错（取到 nil）、页面也不 500，只在生产上表现为「那一块是空的」。
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

func boolPtr(b bool) *bool { return &b }

// salesOverviewTemplateData 一份「有数据」的整页数据。
//
// 键名逐一对齐 orderhttp.salesOverviewView 的返回值 —— 少一个键，模板对应块静默为空。
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
	data["Cards"] = []tmplSalesCard{
		{Label: "订单数", Value: "12", Note: "含没有商品明细的订单；共 23 行明细", Accent: "primary"},
		{Label: "销售额", Value: "¥1,234.50", Note: "商品行实付合计", Accent: "success"},
		{Label: "平均订单价值", Value: "¥102.88", Note: "销售额 ÷ 订单数（AOV）", Accent: "info"},
		{Label: "平均每单件数", Value: "1.92", Note: "商品明细行数 ÷ 订单数", Accent: "mute"},
	}
	data["Clients"] = []tmplSalesCard{
		{Label: "下单客户", Value: "9", Note: "按账号去重", Accent: "primary"},
		{Label: "新客户", Value: "5", Note: "首单落在本区间内", Accent: "success"},
		{Label: "回头客户", Value: "4", Note: "复购率 25.0%（2 人下了 2 单以上）", Accent: "info"},
		{Label: "平均客户价值", Value: "¥137.17", Note: "销售额 ÷ 下单客户数（ACV）", Accent: "mute"},
	}
	data["Compare"] = []tmplSalesCompareRow{
		{Label: "订单数", Prev: "10", Change: "+20.0%", Up: boolPtr(true), UpIsGood: true},
		{Label: "销售额", Prev: "¥900.00", Change: "+37.2%", Up: boolPtr(true), UpIsGood: true},
		// 无基期：Change 空串 + NoBase，模板必须显示「上期无数据」而不是一个空箭头位。
		{Label: "下单客户", Prev: "0", Change: "", NoBase: true},
	}
	data["MonthlyPoints"] = []map[string]any{
		{"Month": "2026-09", "Label": "2026-09", "OrderCount": 10, "Sales": "¥900.00", "Customers": 8, "Height": 120, "Zero": false},
		// 零值月：高度 0 + Zero=true，模板要给它 is-zero（画基线柱而不是留空位）。
		{"Month": "2026-10", "Label": "2026-10", "OrderCount": 12, "Sales": "¥1,234.50", "Customers": 9, "Height": 160, "Zero": false},
		{"Month": "2026-08", "Label": "2026-08", "OrderCount": 0, "Sales": "¥0.00", "Customers": 0, "Height": 0, "Zero": true},
	}
	data["HasMonthly"] = true
	data["MonthlyMaxSales"] = int64(123450)
	return data
}

// renderSalesOverview 渲染整页模板（渲染期错误即失败）。
// 复用本包既有的 renderAdminPage（它已把 t / lang / csrf 之外的变量集处理好）。
func renderSalesOverview(t *testing.T, data map[string]any) string {
	t.Helper()
	return renderAdminPage(t, "admin/order/sales_overview.html", data)
}

// 有数据时：八张卡的标题与值都出现，且口径说明进的是 title（不占卡面）。
func TestSalesOverviewRendersCards(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	for _, want := range []string{
		"订单数", "销售额", "平均订单价值", "平均每单件数",
		"下单客户", "新客户", "回头客户", "平均客户价值",
		"¥1,234.50", "¥102.88", "¥137.17", "1.92",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("页面缺少 %q", want)
		}
	}
	// 口径说明必须落在 title 属性里 —— 卡面上出现它就意味着占了一行。
	if !strings.Contains(out, `title="销售额 ÷ 订单数（AOV）"`) {
		t.Error("AOV 的口径说明没有渲染成 title 悬浮")
	}
	if strings.Contains(out, `<p class="stat-note">`) {
		t.Error("卡面上出现了说明段落（口径应走 title 悬浮）")
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

// 趋势柱：高度写进 style，零值月有 is-zero。
func TestSalesOverviewRendersMonthlyBars(t *testing.T) {
	out := renderSalesOverview(t, salesOverviewTemplateData())

	if !strings.Contains(out, "height:120px") || !strings.Contains(out, "height:160px") {
		t.Error("趋势柱高度没有写进 style")
	}
	if !strings.Contains(out, "sales-bar is-zero") {
		t.Error("零值月没有 is-zero（会被读成漏渲染）")
	}
	for _, want := range []string{"2026-09", "2026-10", "2026-08"} {
		if !strings.Contains(out, want) {
			t.Errorf("横轴缺少刻度 %q", want)
		}
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
	data["Clients"] = []tmplSalesCard{}
	data["Compare"] = []tmplSalesCompareRow{}
	data["MonthlyPoints"] = []map[string]any{}
	data["HasMonthly"] = false
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
	data["Clients"] = []tmplSalesCard{}
	data["Compare"] = []tmplSalesCompareRow{}
	data["MonthlyPoints"] = []map[string]any{}
	data["HasMonthly"] = false
	data["MonthlyMaxSales"] = int64(0)

	out := renderSalesOverview(t, data)

	if !strings.Contains(out, "这段时间没有计入消费的订单。") {
		t.Error("空态文案没有渲染")
	}
	if strings.Contains(out, "+20.0%") {
		t.Error("空态下仍渲染了环比（会给「这个区间有数据」的错觉）")
	}
}

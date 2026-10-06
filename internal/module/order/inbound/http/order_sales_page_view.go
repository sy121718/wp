package orderhttp

// order_sales_page_view.go — 销售概览页的视图组装（把 dto 的事实变成模板能读的键）。
//
// 四条本文件独有的约定：
//
//  1. **展示串只在这里生成**。dto 只给整数分与 float64，`¥1,234.50` / `+12.5%` 这些串
//     都在本文件拼 —— 金额换算全站只有 orderAmountText 一处，
//     同一份 dto 若自带 Label，页面与将来的 AI 工具就会各格式化一次并迟早分叉。
//
//  2. **没有上一期数据时不给「+0.0%」**。`Compare` 为 nil、或某个变化率为 nil 时，
//     模板拿到的是空串 —— 显示 0% 会让「上个月一单没卖」看起来像「这个月跟它持平」。
//
//  3. **文案走 tr（shell.TranslateFor 的取词函数）而不是写死中文**：本页的值是拼出来的
//     （含动态数字），没法整句走模板的 `.["t"](key, fallback)`，所以取词必须在这里做。
//     写死中文会让英文界面上出现一块中文，而那是这一页最容易漏的地方。
//
//  4. **绝不做 /100**：分→元的换算只发生在 orderAmountText 内部（AOV / ACV 也是分）。

import (
	"fmt"
	"strings"

	orderdto "go_wp/internal/module/order/dto"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/i18n"
)

// salesLabelKey / salesNoteKey 本页文案的 key 前缀（模板与视图共用一套）。
const (
	salesLabelPrefix = "admin.order.sales.label."
	salesNotePrefix  = "admin.order.sales.note."
)

// salesCurrency 本页金额的币种（与建单页、订单列表页同一个来源）。
//
// 销售概览跨多张订单，而 `orders.currency` 是**下单当时的快照**（可能有历史币种），
// 所以这里用全局默认币种：本页回答的是「站点在卖多少钱」，不是「某一张单当时是什么币」。
func salesCurrency() string {
	if v := strings.TrimSpace(i18n.GetDefaultCurrency()); v != "" {
		return v
	}
	return "CNY"
}

// salesCardView 一张统计卡。
type salesCardView struct {
	// Label 卡片标题（已译）。
	Label string
	// Value 主数值（已格式化：金额带符号、比率带 %）。
	Value string
	// Note 悬浮说明（口径）。空串表示这张卡不需要解释 —— 模板里 `if .Note` 判断。
	//
	// 口径说明**不占卡面**：KPI 卡上多一行小字会把几张卡的高度撑得参差不齐，
	// 而口径是「读的人想知道时才想知道」的东西（与概览页 KPI 卡同一手法，走 title 悬浮）。
	Note string
	// Accent 卡片左侧强调色（只用于区分维度，不表达好坏）。
	Accent string
}

// salesFilterView 筛选条的回显值。
type salesFilterView struct {
	Project string
	From    string
	To      string
	Status  string
	// StatusLabel 状态筛选的展示文案（空串表示「全部计入消费的状态」）。
	StatusLabel string
	// Monthly 趋势回看月数。
	Monthly int
}

// salesOverviewView 销售概览的整页视图数据。
func salesOverviewView(
	res *orderdto.SalesOverviewResp,
	projects []projectdto.ProjectResp,
	selected string,
	filter salesFilterView,
	tr func(key, fallback string) string,
) map[string]any {
	cur := salesCurrency()
	points := make([]map[string]any, 0, len(res.MonthlyPoints))
	maxSales := int64(0)
	for _, p := range res.MonthlyPoints {
		if p.Sales > maxSales {
			maxSales = p.Sales
		}
	}
	for _, p := range res.MonthlyPoints {
		points = append(points, salesMonthlyPointView(p, cur, maxSales))
	}
	return map[string]any{
		"Projects":        projects,
		"SelectedProject": selected,
		"Filter":          filter,

		"From":    res.From,
		"To":      res.To,
		"Status":  res.Status,
		"Monthly": res.Monthly,
		"HasData": res.OrderCount > 0 || res.Monthly > 0,

		"Cards":   salesCardViews(res, cur, tr),
		"Clients": salesClientViews(res, cur, tr),
		"Compare": salesCompareView(res.Compare, cur, tr),

		"MonthlyPoints":   points,
		"HasMonthly":      len(points) > 0,
		"MonthlyMaxSales": maxSales,
	}
}

// salesCardViews 四张销售卡（订单 / 销售额 / 平均订单价值 / 平均每单件数）。
//
// 卡的顺序是**从「一共多少」到「单笔多大」**：先总量、后均值的读法是报表的通行约定，
// 反过来（先 AOV 再总量）会让读的人先看到一个需要上下文才能判断大小的数。
func salesCardViews(res *orderdto.SalesOverviewResp, cur string, tr func(key, fallback string) string) []salesCardView {
	return []salesCardView{
		{
			Label:  tr(salesLabelPrefix+"orders", "订单数"),
			Value:  fmt.Sprintf("%d", res.OrderCount),
			Note:   fmt.Sprintf(tr(salesNotePrefix+"orders", "含没有商品明细的订单；共 %d 行明细"), res.ItemRows),
			Accent: "primary",
		},
		{
			Label:  tr(salesLabelPrefix+"sales", "销售额"),
			Value:  orderMoneyLabel(res.Sales, cur),
			Note:   tr(salesNotePrefix+"sales", "商品行实付合计，不含退款分摊（净销售额是另一个口径）"),
			Accent: "success",
		},
		{
			Label:  tr(salesLabelPrefix+"aov", "平均订单价值"),
			Value:  orderMoneyLabel(res.AvgOrderValue, cur),
			Note:   tr(salesNotePrefix+"aov", "销售额 ÷ 订单数（AOV）"),
			Accent: "info",
		},
		{
			Label:  tr(salesLabelPrefix+"units", "平均每单件数"),
			Value:  fmt.Sprintf("%.2f", res.AvgItemsPerOrder),
			Note: fmt.Sprintf(tr(salesNotePrefix+"units", "商品明细行数 ÷ 订单数；不等于件数（一行可能多件），本区间共 %d 件"),
				res.Units),
			Accent: "mute",
		},
	}
}

// salesClientViews 四张客户卡（下单客户 / 新客户 / 回头客户 / 平均客户价值）。
//
// 「复购率」不做成第五张卡而是并进回头客那张的说明里：它与回头客是同一个问题的两面
// （一个给人数、一个给比例），并成两张卡会让读的人以为是两组不同的人。
func salesClientViews(res *orderdto.SalesOverviewResp, cur string, tr func(key, fallback string) string) []salesCardView {
	return []salesCardView{
		{
			Label:  tr(salesLabelPrefix+"customers", "下单客户"),
			Value:  fmt.Sprintf("%d", res.Customers),
			Note:   tr(salesNotePrefix+"customers", "按账号去重；游客单不计入（没有账号可归）"),
			Accent: "primary",
		},
		{
			Label:  tr(salesLabelPrefix+"new", "新客户"),
			Value:  fmt.Sprintf("%d", res.NewCustomers),
			Note:   tr(salesNotePrefix+"new", "首单落在本区间内（不看注册时间）"),
			Accent: "success",
		},
		{
			Label: tr(salesLabelPrefix+"returning", "回头客户"),
			Value: fmt.Sprintf("%d", res.ReturningCustomers),
			Note: fmt.Sprintf(tr(salesNotePrefix+"returning", "首单在区间之前、区间内又下单；复购率 %.1f%%（%d 人下了 2 单以上）"),
				res.RepurchaseRatePct, res.Repurchasers),
			Accent: "info",
		},
		{
			Label:  tr(salesLabelPrefix+"acv", "平均客户价值"),
			Value:  orderMoneyLabel(res.AvgCustomerValue, cur),
			Note:   tr(salesNotePrefix+"acv", "销售额 ÷ 下单客户数（ACV）；分母是人不是单，与 AOV 不同"),
			Accent: "mute",
		},
	}
}

// salesCompareRowView 环比的一个指标（上一期绝对值 + 变化率 + 方向）。
//
// **只给上一期的绝对值**，本期值不重复塞进来：本期三个数已经在上面那排卡片里
// （订单数 / 销售额 / 下单客户），同一屏里放两份就会在两次渲染之间分叉 ——
// 而读的人会先看到两个不一样的数字，再开始怀疑哪个是对的。
type salesCompareRowView struct {
	Label string
	// Prev 上一期的绝对值（已格式化）。
	Prev string
	// Change 变化率文本（`+12.5%` / `-3.0%` / **空串**（上一期为 0））。
	Change string
	// Up 方向：true 上升 / false 下降 / **nil 时模板不画箭头**。
	//
	// 用 *bool 不用 bool：nil（无基期）时不能显示成「下降」。
	Up *bool
	// UpIsGood 上升是否算好事。
	//
	// 本页三个指标都是「多比少好」，所以恒为 true；单独留一个字段是为了
	// 将来加「退款率」这类指标时不会默认染成绿色 —— 那是一个会静默误导人的默认值。
	UpIsGood bool
	// NoBase 上一期为 0（变化率算不出来）。模板据此显示「上期无数据」而不是空的箭头位。
	NoBase bool
}

// salesCompareView 环比区块（上一期为 nil 时返回 nil，模板整块不渲染）。
func salesCompareView(cmp *orderdto.SalesCompareDTO, cur string, tr func(key, fallback string) string) []salesCompareRowView {
	if cmp == nil {
		return nil
	}
	return []salesCompareRowView{
		{
			Label:    tr(salesLabelPrefix+"orders", "订单数"),
			Prev:     fmt.Sprintf("%d", cmp.OrderCount),
			Change:   salesChangeText(cmp.OrderCountChangePct),
			Up:       salesChangeUp(cmp.OrderCountChangePct),
			UpIsGood: true,
			NoBase:   cmp.OrderCountChangePct == nil,
		},
		{
			Label:    tr(salesLabelPrefix+"sales", "销售额"),
			Prev:     orderMoneyLabel(cmp.Sales, cur),
			Change:   salesChangeText(cmp.SalesChangePct),
			Up:       salesChangeUp(cmp.SalesChangePct),
			UpIsGood: true,
			NoBase:   cmp.SalesChangePct == nil,
		},
		{
			Label:    tr(salesLabelPrefix+"customers", "下单客户"),
			Prev:     fmt.Sprintf("%d", cmp.Customers),
			Change:   salesChangeText(cmp.CustomersChangePct),
			Up:       salesChangeUp(cmp.CustomersChangePct),
			UpIsGood: true,
			NoBase:   cmp.CustomersChangePct == nil,
		},
	}
}

// salesChangeText 变化率 → 展示串（nil → 空串）。
func salesChangeText(pct *float64) string {
	if pct == nil {
		return ""
	}
	return fmt.Sprintf("%+.1f%%", *pct)
}

// salesChangeUp 变化率 → 方向（nil 或恰好 0 → nil，不画箭头）。
//
// 恰好 0 也给 nil：0.0% 画一个向下的箭头是错的，而不画箭头时那个 `0.0%`
// 本身已经说清「没变」。
func salesChangeUp(pct *float64) *bool {
	if pct == nil {
		return nil
	}
	if *pct > 0 {
		up := true
		return &up
	}
	if *pct < 0 {
		up := false
		return &up
	}
	return nil
}

// salesMonthlyPointView 趋势图的一个柱（含按最高值归一化的高度）。
//
// 高度在这里算而不是在模板里：Jet 的算术能力有限，而 `h = sales / max * H`
// 这种式子写在模板里出错时不会报错、只会画出一根高度诡异的柱子。
func salesMonthlyPointView(p orderdto.SalesMonthlyPointDTO, cur string, maxSales int64) map[string]any {
	height := 0
	if maxSales > 0 && p.Sales > 0 {
		height = int(p.Sales * int64(salesChartMaxHeight) / maxSales)
	}
	return map[string]any{
		"Month":      p.Month,
		"Label":      salesMonthLabel(p.Month),
		"OrderCount": p.OrderCount,
		"Sales":      orderMoneyLabel(p.Sales, cur),
		"Customers":  p.Customers,
		"Height":     height,
		// Zero 为真时模板画一根 1px 的基线柱（而不是完全不画）：
		// 某个整月是 0 时，读者需要看到「这里有个柱子，只是它是 0」，
		// 而不是怀疑那一格漏渲染了。
		"Zero": p.Sales == 0,
	}
}

// salesMonthLabel 横轴刻度（原样返回 `2026-10`）。
//
// **刻意不做成「10月」或「2026年10月」**：中文年月会让英文界面显示中文，
// 而做一整套月份名 i18n（12 个 key × 语言数）为一个横轴刻度不值得。
// `2026-10` 是国际通用的机器可读写法，跨语言都不会被误读，也不会出现
// 跨年时两个「1月」分不清先后的情况。
func salesMonthLabel(month string) string {
	if strings.TrimSpace(month) == "" {
		return orderFieldEmpty
	}
	return month
}

// salesChartMaxHeight 趋势柱的最大像素高度（与模板里 svg 的绘图区高度一致）。
const salesChartMaxHeight = 160

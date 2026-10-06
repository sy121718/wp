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
	"strconv"
	"strings"

	orderdto "go_wp/internal/module/order/dto"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/i18n"
)

// salesLabelKey / salesNoteKey 本页文案的 key 前缀（模板与视图共用一套）。
const (
	salesLabelPrefix = "admin.order.sales.label."
	salesNotePrefix  = "admin.order.sales.note."
	// salesMixPrefix / salesMonthlyPrefix 是既有的两组 key（迁移 581）：
	// 客户类型拆分与月度合计在柱图那版就种好了，这里只换图的形态，不另立词条。
	salesMixPrefix     = "admin.order.sales.mix."
	salesMonthlyPrefix = "admin.order.sales.monthly."
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
		"Compare": salesCompareView(res.Compare, cur, tr),
		"Trend":   salesTrendView(res.MonthlyPoints, cur, tr),
	}
}

// salesCardView 之外，本页只有一条趋势图 —— 详情见 salesTrendView。

// salesCardViews 四张卡：订单数 / 销售额 / 平均订单价值 / 平均客户价值。
//
// 卡的顺序是**从「一共多少」到「单笔 / 单人多大」**：先总量、后均值的读法是报表的通行约定，
// 反过来（先 AOV 再总量）会让读的人先看到一个需要上下文才能判断大小的数。
//
// **为什么只留这四张**：本页讲的是「卖了多少钱」，客户侧只有 ACV 是金额口径。
// 下单客户 / 新客户 / 回头客户是「人数」，与前三张不同量纲、也不回答本页的问题；
// 平均每单件数更是商品侧的口径。它们挤在同一排里会让「八张卡讲一件事」变成
// 「八张卡各讲各的」，读的人反而看不出重点。
// 账号维度的客户数没有消失 —— 环比区块里仍有「下单客户」那一行，趋势图也有客户类型拆分。
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
			Label:  tr(salesLabelPrefix+"acv", "平均客户价值"),
			Value:  orderMoneyLabel(res.AvgCustomerValue, cur),
			Note:   tr(salesNotePrefix+"acv", "销售额 ÷ 下单客户数（ACV）；分母是人不是单，与 AOV 不同"),
			Accent: "mute",
		},
	}
}

// ── 月度趋势（折线图）────────────────────────────────────────────────────
//
// 坐标一律在这里算完，模板只贴字符串 —— 与 AI 会话页的 token 趋势同一套规矩，
// 两处共用 .chart-trend-* 的样式（viewBox 1000×200 + preserveAspectRatio="none"）。

const (
	// salesTrendViewW / salesTrendViewH 与模板里的 viewBox 一致。
	salesTrendViewW = 1000
	salesTrendViewH = 200
	// salesTrendPadX 左右各留一点：x=0 的点描边有一半落在 viewBox 外，会被裁掉半条线。
	salesTrendPadX = 6
	// salesTrendPadTop 顶部留白：峰值那一点不能贴到上边界。
	salesTrendPadTop = 12
	// salesTrendAxisH 底部月份刻度占的高度（绘图区必须让开，否则曲线会压在刻度上）。
	salesTrendAxisH = 40
)

// salesTrendSeries 折线图上的一条线。
type salesTrendSeries struct {
	// Label 线名（已译）。
	Label string
	// Color 1..8 的调色板序号，模板映射到 --chart-cN。
	Color int
	// Points 是 `<polyline points="…">` 的现成内容（"x,y x,y …"）。
	Points string
	// Total 区间合计（已格式化），图例右侧显示。
	Total string
	// Dots 每个数据点的命中块 + 悬停文案。
	//
	// 为什么要有它：`<polyline>` 只能挂一个 `<title>`，整条线一句话说不清「这个月多少」。
	// 而这是本页唯一的读数入口 —— 没有它，图上就只有形状没有数字。
	// 块取 14×14 而不是 hover 描边命中：折线本身细，鼠标要精确压在 2px 上才触发。
	Dots []salesTrendDot
}

// salesTrendDot 折线上的一个数据点（命中块中心 + 悬停文案）。
type salesTrendDot struct {
	X     int
	Y     int
	Title string
}

// salesTrendAxis 横轴的一个刻度。
type salesTrendAxis struct {
	X     int
	Label string
	// Anchor text-anchor：首尾两个刻度贴边会出界，往内收。
	Anchor string
}

// salesTrendView 组装折线图（没有月份时 HasData=false，模板不渲染画布）。
type salesTrendChart struct {
	HasData bool
	// HasSales 任一月份有销售额。与 HasData 是两件事：回看窗口天生总有月份（补零过），
	// 所以 HasData 几乎恒真；一笔销售都没有时画一块空画布会被读成「趋势平缓」。
	HasSales bool
	Series   []salesTrendSeries
	Axis     []salesTrendAxis
	// Peak/PeakLabel 峰值与其格式化金额（纵轴上限的读数）。
	Peak      int64
	PeakLabel string
}

func salesTrendView(points []orderdto.SalesMonthlyPointDTO, cur string, tr func(key, fallback string) string) salesTrendChart {
	out := salesTrendChart{}
	if len(points) == 0 {
		return out
	}
	out.HasData = true

	// 纵轴上限取「单月总额」的最大值（不是三段各自的最大值）：
	// 三条线共用一个刻度才可比 —— 各自归一化会让「游客单比新客高」这种事实看不出来。
	var peak int64
	for _, p := range points {
		if p.Sales > peak {
			peak = p.Sales
		}
		if p.Sales > 0 {
			out.HasSales = true
		}
	}
	out.Peak = peak
	out.PeakLabel = orderMoneyLabel(peak, cur)

	plotH := salesTrendViewH - salesTrendPadTop - salesTrendAxisH
	xOf := func(i int) int {
		if len(points) == 1 {
			return salesTrendViewW / 2
		}
		return salesTrendPadX + i*(salesTrendViewW-2*salesTrendPadX)/(len(points)-1)
	}
	yOf := func(cents int64) int {
		if peak <= 0 || cents <= 0 {
			return salesTrendPadTop + plotH
		}
		return salesTrendPadTop + plotH - int(cents*int64(plotH)/peak)
	}

	type seg struct {
		label string
		color int
		value func(orderdto.SalesMonthlyPointDTO) int64
	}
	segs := []seg{
		{
			label: tr(salesMixPrefix+"new", "新客"),
			color: 3,
			value: func(p orderdto.SalesMonthlyPointDTO) int64 { return p.NewSales },
		},
		{
			label: tr(salesMixPrefix+"returning", "回头客"),
			color: 1,
			value: func(p orderdto.SalesMonthlyPointDTO) int64 { return p.ReturningSales },
		},
		{
			label: tr(salesMixPrefix+"guest", "游客单"),
			color: 8,
			value: func(p orderdto.SalesMonthlyPointDTO) int64 { return p.GuestSales },
		},
	}

	out.Series = make([]salesTrendSeries, 0, len(segs))
	for _, s := range segs {
		pts := make([]string, 0, len(points))
		dots := make([]salesTrendDot, 0, len(points))
		var total int64
		for i, p := range points {
			v := s.value(p)
			total += v
			x, y := xOf(i), yOf(v)
			pts = append(pts, strconv.Itoa(x)+","+strconv.Itoa(y))
			dots = append(dots, salesTrendDot{
				X: x,
				Y: y,
				Title: fmt.Sprintf("%s｜%s %s｜%s %s",
					salesMonthLabel(p.Month), s.label, orderMoneyLabel(v, cur),
					tr(salesMonthlyPrefix+"total", "合计"), orderMoneyLabel(p.Sales, cur)),
			})
		}
		out.Series = append(out.Series, salesTrendSeries{
			Label:  s.label,
			Color:  s.color,
			Points: strings.Join(pts, " "),
			Total:  orderMoneyLabel(total, cur),
			Dots:   dots,
		})
	}

	out.Axis = make([]salesTrendAxis, 0, len(points))
	for i, p := range points {
		anchor := "middle"
		if i == 0 {
			anchor = "start"
		} else if i == len(points)-1 {
			anchor = "end"
		}
		out.Axis = append(out.Axis, salesTrendAxis{
			X:      xOf(i),
			Label:  salesMonthLabel(p.Month),
			Anchor: anchor,
		})
	}
	return out
}

// salesCardViews 之外的客户侧口径：账号维度的客户数只在环比区块与趋势图里出现。
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

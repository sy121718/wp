package orderdto

// SalesOverviewResp 销售概览的全部结论。
type SalesOverviewResp struct {
	ProjectID string `json:"projectId"`
	// From / To **实际生效**的窗口（YYYY-MM-DD，均为含当天）。回显生效值而不是请求值：
	// 请求可能给了未来日期（会被收敛到今天），页面标题要写「哪一段」，写请求值就是一句错的说明。
	From string `json:"from"`
	To   string `json:"to"`
	// Status 实际生效的状态筛选（空串表示取了默认的三个计入消费的状态）。
	Status string `json:"status"`
	// Monthly 实际生效的趋势回看月数。
	Monthly int `json:"monthly"`
	// Currency 本报表金额的币种代码（来自站点默认币种）。
	//
	// 报表必须说清自己用哪种币种：金额一律是整数分，不带币种前缀的「1234.50」在
	// 多币种站点上无法解读。页面据此拼前缀（`money(cents, currencyPrefix(currency))`）。
	Currency string `json:"currency"`

	// ── 销售维度（区间内）────────────────────────────────────────────

	// OrderCount 区间内的订单数（**含没有明细行的订单**）。
	OrderCount int64 `json:"orderCount"`
	// ItemRows 订单商品行数（一单多行各算一次）。
	ItemRows int64 `json:"itemRows"`
	// Units 售出件数（Σ 明细数量）。
	Units int64 `json:"units"`
	// Sales 销售额（分，**行实付合计，不含退款分摊**）—— 与「净销售额」不是同一个口径，
	// 所以标签必须分别叫「销售额」与「净销售额」，同名会让运营以为其中一个算错了。
	Sales int64 `json:"sales"`

	// AvgOrderValue 平均订单价值 AOV（分）= 销售额 / 订单数；订单数为 0 时为 0。
	AvgOrderValue int64 `json:"avgOrderValue"`
	// AvgCustomerValue 平均客户价值 ACV（分）= 销售额 / 下单客户数。
	//
	// 与 AOV 的分母不同（订单 vs 人）：一个人下三单时 AOV 与 ACV 差三倍，
	// 这两个数放在同一排卡片上时必须各自带标签 —— 都叫「客单价」时读的人无法分辨。
	AvgCustomerValue int64 `json:"avgCustomerValue"`
	// AvgItemsPerOrder 平均每单商品行数（保留两位小数，如 2.35）。
	AvgItemsPerOrder float64 `json:"avgItemsPerOrder"`
	// AvgSalesPerUnit 平均每件销售额（分）= 销售额 / 售出件数。
	AvgSalesPerUnit int64 `json:"avgSalesPerUnit"`

	// ── 毛利（迁移 632，W4）────────────────────────────────────────

	// GrossProfit 毛利（分）= Σ 行实付 − Σ 行成本 − Σ 运费。
	//
	// 口径（写在这里供报表侧核对）：**税不扣**（tax_total 是代收的，不是成本）、
	// 优惠已含在 line_total 里（不重复扣）、运费按订单去重后扣一次。
	GrossProfit int64 `json:"grossProfit"`
	// CostIncomplete 有明细行的 cost_price 为 NULL（= 成本未核算）。
	//
	// 卡片的判据：为真时显示「含 N 行成本未核算」，而不是悄悄把毛利当成准的。
	// 那些行按 0 计入毛利 —— 用 0 冒充未知会让毛利虚高，比不显示更糟。
	CostIncomplete bool `json:"costIncomplete"`
	// CostIncompleteRows 成本未核算的明细行数（CostIncomplete 的具体条数）。
	CostIncompleteRows int64 `json:"costIncompleteRows"`

	// ── 客户维度（区间内）────────────────────────────────────────────

	// Customers 下单客户数（按 user_id 去重，**不含游客单**）。
	Customers int64 `json:"customers"`
	// NewCustomers 首单落在区间内的客户数（不看注册时间）。
	NewCustomers int64 `json:"newCustomers"`
	// ReturningCustomers 首单在区间之前、区间内又下单的客户数。
	ReturningCustomers int64 `json:"returningCustomers"`
	// Repurchasers 区间内下了 ≥2 单的客户总数（不分新老）。
	Repurchasers int64 `json:"repurchasers"`
	// RepurchaseRatePct 复购率（百分比，一位小数）；分母为 0 时为 0（不是 NaN）。
	RepurchaseRatePct float64 `json:"repurchaseRatePct"`

	// ── 环比上一期 ─────────────────────────────────────────────────

	// Compare 与**紧邻的上一段等长区间**的对比；上一期没有任何数据时为 nil
	//（不显示一排 -100%，那会让「上个月没卖东西」看起来像「这个月暴跌」）。
	Compare *SalesCompareDTO `json:"compare,omitempty"`

	// ── 月度趋势 ───────────────────────────────────────────────────

	// Converted 多币种折算（迁移 633，W5）：只有「与基准币不同且有快照」的订单参与折算。
	//
	// UnconvertedOrders 是**必须显示**的：折算金额只覆盖有快照的那部分，
	// 不说明「有多少单没折算」会让一个偏小的数字被当成全量。
	Converted *ConvertedOverview `json:"converted,omitempty"`

	// MonthlyPoints 近 Monthly 个月（含当月）逐月补零的序列，升序。
	//
	// **口径与卡片不同**（见 SalesOverviewReq.Monthly 的说明）：卡片看筛选区间、
	// 趋势看固定回看窗口。所以两个数字**不该对得上**，页面上也不做「卡片之和 == 趋势之和」
	// 的暗示 —— 那正是最容易被当成 bug 报上来的一类不一致。
	MonthlyPoints []SalesMonthlyPointDTO `json:"monthlyPoints"`
}

// SalesMonthlyPointDTO 月度趋势的一个桶。
type SalesMonthlyPointDTO struct {
	// Month 桶键 YYYY-MM（UTC 月）。
	Month string `json:"month"`
	// OrderCount 该月订单数。
	OrderCount int64 `json:"orderCount"`
	// Sales 该月销售额（分）。
	Sales int64 `json:"sales"`
	// Customers 该月下单客户数（按账号去重，不含游客单）。
	Customers int64 `json:"customers"`

	// ── 按客户类型的拆分（口径见 SalesOverviewResp 的说明）────────────────
	//
	// **三段之和恒等于上面的总量**：NewXxx + ReturningXxx + GuestXxx == Xxx。
	// 这是本页唯一一条「拆开的数字必须能拼回去」的判据，由
	// TestMonthlyCustomerMixSumsToTotal 钉住 —— 只断言各段的值时，
	// 改一段的判据、漏掉另一段，测试仍然全绿。

	// NewOrderCount 该月新客订单数（客户首单落在这个自然月）。
	NewOrderCount int64 `json:"newOrderCount"`
	// ReturningOrderCount 该月回头客订单数。
	ReturningOrderCount int64 `json:"returningOrderCount"`
	// GuestOrderCount 该月游客订单数（没有账号，既非新客也非回头客）。
	GuestOrderCount int64 `json:"guestOrderCount"`
	// NewSales 该月新客订单销售额（分）。
	NewSales int64 `json:"newSales"`
	// ReturningSales 该月回头客订单销售额（分）。
	ReturningSales int64 `json:"returningSales"`
	// GuestSales 该月游客订单销售额（分）。
	GuestSales int64 `json:"guestSales"`
	// NewCustomers 该月下过单的新客数。
	NewCustomers int64 `json:"newCustomers"`
	// ReturningCustomers 该月下过单的回头客数。
	ReturningCustomers int64 `json:"returningCustomers"`
}

// SalesCompareDTO 与上一段等长区间的对比。
//
// 三个变化率都是**相对于上一期**的百分比（一位小数），上一期为 0 时为 nil（不是 0%）：
// 「从 0 涨到 100」的变化率是无穷大而不是 100%，写 0% 或者 +∞ 都是错的答案。
type SalesCompareDTO struct {
	// From / To 上一期的窗口（YYYY-MM-DD，含当天）。
	From string `json:"from"`
	To   string `json:"to"`
	// OrderCount / Sales / Customers 上一期的绝对值（页面要写「上期 12 单 / ¥800.00」，
	// 只有变化率时读的人无法判断这个 +100% 是从 1 单涨到 2 单还是从 100 涨到 200）。
	OrderCount int64 `json:"orderCount"`
	Sales      int64 `json:"sales"`
	Customers  int64 `json:"customers"`
	// OrderCountChangePct 订单数变化率（%）；上一期为 0 时为 nil。
	OrderCountChangePct *float64 `json:"orderCountChangePct,omitempty"`
	// SalesChangePct 销售额变化率（%）；上一期为 0 时为 nil。
	SalesChangePct *float64 `json:"salesChangePct,omitempty"`
	// CustomersChangePct 客户数变化率（%）；上一期为 0 时为 nil。
	CustomersChangePct *float64 `json:"customersChangePct,omitempty"`
}

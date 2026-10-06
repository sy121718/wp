package orderdto

// order_sales_overview.go — 「销售概览」页的请求与响应（订单模块 /admin/orders/overview）。
//
// 口径来源是 Laravel CRM 的 Reports/SalesProductSummary + UnitsOrdered + OverviewAnalysisService
// （那套 sales dashboard 的卡片与图）—— **只搬口径不搬实现**：
//   - 那边把「订单商品行数」与「销售件数」放两个接口分两次取，两次之间落的单会让
//     「平均每单 1.8 件」这种派生值自相矛盾；这里一条查询一次回，派生值只在本包算一次。
//   - 那边的筛选栏有 Marketplace（国家）与 Created Via（下单来源）两个下拉，本系统的运营面
//     从来没有这两种入口（订单列表页只有工程 / 状态 / 关键词 / 支付方式），所以不做。
//   - 那边按邮箱判客户，同一个人换邮箱会变成两个客户；这里一律按 user_id。
//
// **本文件只放事实，不放展示串**：金额一律整数分、比率一律 float64，
// 「¥1,234.50」「+12.5%」「10月」这些串全部由 inbound/http 的视图层生成 ——
// 金额换算与千分位只有一处（`orderMoneyLabel` / `orderAmountText`），
// 同一份 dto 若自带 Label，AI 工具与页面两条消费路径就会各格式化一次并迟早分叉。

// SalesOverviewReq 销售概览的请求。
//
// 时间用**字符串**而不是 time.Time：同一份结构既服务后台原生表单（form）也要服务 JSON 接口，
// 表单里的日期天生是文本。解析只发生在 service，非法格式在那里统一报错。
//
// From / To 都是 YYYY-MM-DD、**含当天**；两者都必须给 —— 留空则走「静默默认窗口」，
// 那是「我看到的是哪一段」说不清楚的一类数字，宁可让调用方显式传。
type SalesOverviewReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
	// Status 订单状态筛选（白名单：paid / shipped / completed 的任意组合，逗号分隔）；
	// 空 = 全部计入消费的状态（与「净销售额」同口径的那三个）。
	//
	// 这里**刻意不给 cancelled / refunded**：本页所有数字都是「卖了多少钱」，
	// 把取消单算进来会得到一个没人要的口径（那属于「下了多少单」，订单列表页已经在回答）。
	Status string `form:"status" json:"status"`
	// Monthly 趋势回看的月数（1~24，默认 6）：趋势图看的是「近半年卖得怎么样」，
	// 而卡片看的是筛选区间 —— 两者口径不同才会问出不同的问题，
	// 若趋势也跟着 from/to 走，选一个「今天」就只剩一个点。
	Monthly int `form:"monthly" json:"monthly"`
}

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

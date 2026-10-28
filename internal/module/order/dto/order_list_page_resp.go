package orderdto

// order_list_page_resp.go — 订单列表页的渲染视图（页面 GET 的回执形状）。
//
// 与 order_resp.go 的分工同 sales：接口契约 vs 页面视图。
// **导出字段名就是模板契约**（orders.html 的 `.Field`）。

import (
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/pagination"
)

// StatusTabResp 状态筛选行的徽章（取值 + 分档）。
//
// 字段叫 Kind 而不是 Tone：`Tone` 在 internal/architecture 的「dto 不带展示字段」名单里
// （那条判据针对的是「把样式类名/展示分档做成 dto 字段」），而这里**确实是**分档 ——
// 但它是模板拼 `badge badge-{{...}}` 所需的**取值**，不是成品类名。用 Kind 表达同一件事，
// 名字不落在那张名单上，语义更准（它是"哪一档"，不是"什么色调"）。
type StatusTabResp struct {
	Value string
	Kind  string
}

// OrderListPageResp 订单列表整页的渲染视图。
type OrderListPageResp struct {
	Title string
	Menu  string

	Projects        []projectcontract.ProjectResp
	SelectedProject string

	// 取数结果：dto 原样交给模板。
	Orders []*OrderResp
	Counts map[string]int64
	Total  int64
	Detail *OrderDetailResp

	// StatusTabs 状态徽章行的「取值 + 分档」；CountsAll 是「全部」的计数（各状态之和）。
	// 求和放在这里而不是模板里：模板做跨类型累加不值得押注。
	StatusTabs []StatusTabResp
	CountsAll  int64

	// OrderStatusLabel 状态 → 当前语言标签的取词函数（语言绑定行为的已知存量，见 sales 的说明：
	// 改成「预先算好每行标签」要同时动模板与三处调用点，与"换出口形态"分开做）。
	OrderStatusLabel func(string) string
	// CountryLabel 国家/地区代码 → 当前语言显示名。模板里 `{{ .CountryLabel(code) }}`
	// 调用它，详情不必预先翻好每一段地址。
	CountryLabel func(string) string

	FilterStatus  string
	FilterKeyword string
	FilterPayment string

	// FulfillmentTabs 履约状态的徽章行（B7）：取值 + 分档，与 StatusTabs 同形状。
	//
	// 与 StatusTabs 并列而不是合并：两条正交的事实轴各有自己的筛选维度，
	// 合成一行会让「已付款 且 部分发货」这种组合无法表达。
	FulfillmentTabs []StatusTabResp
	// FulfillmentCounts 各履约状态的计数（角标用；已补齐零值，模板可安全比较）。
	FulfillmentCounts map[string]int64
	// FulfillmentLabels 履约状态 → 词条事实（模板用 tr(key, fallback) 取词）。
	//
	// 用 map 而不是函数：履约状态只有 12 个固定取值，模板要在筛选行与列表行两处取词，
	// 预先摊平成事实表比每个调用点过一道闭包更直白（与退货页 ReturnStatusLabels 同形）。
	FulfillmentLabels map[string]StatusLabelFact
	FilterFulfillment string
	// CreatedViaValues / FilterCreatedVia 来源筛选（自建 / 线上 / 接口）。
	CreatedViaValues []string
	FilterCreatedVia string
	// CreatedViaLabels 来源 → 词条事实（复用迁移 448 的 admin.orders.created_via.*）。
	CreatedViaLabels map[string]StatusLabelFact

	// LoadErr 这一次没读出来的原因（空串 = 正常）：模板据此把「装载失败」与「还没有订单」分开。
	LoadErr string
	// ListQuery 列表上下文（拼进页内链接的 query；不含 status）。
	ListQuery string

	Page  int
	Limit int

	// Pagination 分页事实（由 adminui.AttachPaginationView 反射挂上，键名 = view.PaginationKey）。
	Pagination pagination.Page
}

// CouponListPageResp 优惠码列表整页的渲染视图。
type CouponListPageResp struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string

	Coupons []*CouponResp
	Total   int64
	// Expanded 展开中的券（nil = 没有展开项；模板按 .Expanded.ID 比较）。
	Expanded        *CouponResp
	Redemptions     []*CouponRedemptionResp
	RedemptionTotal int64
	RedemptionLimit int

	// 筛选：白名单值给模板画下拉，原值给回显
	// （白名单外的值要原样显示，不能悄悄变成「全部」）。
	StatusValues      []string
	FilterStatus      string
	FilterStatusValid bool
	FilterKeyword     string

	// LoadErr 这一次没读出来的原因（空串 = 正常）。
	LoadErr string
	// ListQuery 列表上下文（拼进抽屉/表单链接的 query）。
	// **没有**回填槽（旧写法的 Echo / SubmitErr）—— 写路径已迁 JSON：
	// 失败时 DOM 里的输入原样保留，不需要服务端重渲片段回填。
	ListQuery string

	Page  int
	Limit int

	Pagination pagination.Page
}

// CouponEditFormView 优惠码编辑抽屉片段的视图（**片段出口**，不是整页）。
//
// 只读 .Coupon（dto 原样）与 .ListQuery：写路径已迁 JSON，没有「失败重渲 + 回填」那条路，
// 所以既不需要 Echo（本次提交的原始表单），也不需要 SubmitErr（服务端算好的错误句）。
type CouponEditFormView struct {
	Coupon    *CouponResp
	ListQuery string
}

// StatusLabelFact 状态 → 词条事实（key + 中文兜底），由模板用 tr(key, fallback) 取词。
//
// 与 StatusTabResp 的区别：那个带「分档」（拼 badge 类），这个只承载**文案事实**。
// 两者都进 dto 而不进 gin.H：模板读 `.Field` 时，字段名拼错在 gin.H 里是静默缺键。
type StatusLabelFact struct {
	Key      string
	Fallback string
}

// ReturnWarehouseResp 入库仓下拉的一项（只给启用的仓）。
//
// 停用的仓历史流水仍按 id 可读，但新入库不该再选它 —— 所以这里只列启用的。
type ReturnWarehouseResp struct {
	ID   string
	Name string
}

// ReturnListPageResp 退货列表整页的渲染视图。
type ReturnListPageResp struct {
	Title           string
	Menu            string
	Projects        []projectcontract.ProjectResp
	SelectedProject string

	Returns []*ReturnResp
	Counts  map[string]int64
	Total   int64
	// CountsAll 「全部」徽章的计数（各状态之和）：模板里跨类型累加（int + int64）
	// 不值得押注，求和放 handler 一行，模板只取结果。
	CountsAll int64
	Detail    *ReturnDetailResp

	// StatusTabs 状态徽章行（取值 + 分档）+ 当前生效值。
	StatusTabs []StatusTabResp
	// ReturnStatusLabels / OrderStatusLabels 状态 → 词条事实（取词在模板调用点发生）。
	ReturnStatusLabels map[string]StatusLabelFact
	OrderStatusLabels  map[string]StatusLabelFact

	FilterStatus  string
	FilterKeyword string
	FilterOrderID uint64

	// LoadErr 这一次没读出来的原因（空串 = 正常）。
	LoadErr string
	// ListQuery 列表上下文（拼进表单 action / 徽章链接；不含 status）。
	ListQuery string

	// Warehouses 入库仓下拉：只给启用的仓。
	Warehouses []ReturnWarehouseResp

	// CountryLabel 国家/地区代码 → 当前语言显示名。
	//
	// **必须显式给**：模板在 `{{if o.ShipAddress}}` 分支里调 `.CountryLabel(...)`，
	// 而 Jet 对「数据里没有这个键就当作函数调用」的处理是**渲染失败 → 整页 500**。
	// 换句话说，只要选中一张订单有地址的退货单，审核入口就整个打不开 ——
	// 这个坑只用真渲染 + 真数据才照得出来（只渲染 returnId 不存在的那类页时
	// detail 分支根本不进，缺陷一直隐形）。
	CountryLabel func(string) string

	Page  int
	Limit int

	Pagination pagination.Page
}

// OrderNewPageResp 订单新建整页的渲染视图。
type OrderNewPageResp struct {
	Title string
	Menu  string

	// 工程上下文（页头下拉；只有一个工程时整段不渲染 —— 单选项的选择器不提供信息）。
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	// LoadFailed 工程列表没读出来；NoProject 没有可选工程（**不是**「没读出来」）；
	// FormReady 表单可以填（两者都为假时才为真）。
	LoadFailed bool
	NoProject  bool
	FormReady  bool

	// 候选明细（同一份候选给每一行复用：模板按行渲染 N 份 option 列表）。
	CandidateKeyword string
	Candidates       []SkuOptionResp
	CandidateCount   int
	CandidateError   string
	// ItemRows 明细行（表单行结构在 handler 里，模板按 .Field 读）。
	ItemRows []OrderNewItemRowResp
	MaxRows  int

	// 国家/地区下拉的选项（含首位「（不填写）」空项，国家是选填）。
	ShipCountries []CountryOptionResp
	BillCountries []CountryOptionResp

	// 币种下拉的选项（迁移 633 之后后台可代客建外币单）。
	//
	// 首位是「用站点默认币」空项 —— 与国家的「（不填写）」不同：那不是一个「没值」的
	// 档位，而是一个确定的语义（订单币取站点默认，此时不写汇率快照）。
	CurrencyOptions []CountryOptionResp

	// RowAddURL 加一行的链接（带当前表单的查询串）。
	RowAddURL string

	// Err 页级错误提示。
	Err string
	// FieldErrors 字段级错误（**稀疏** map：模板用 chain 索引读，缺键 falsy；
	// 有文案即标红，不需要单独的 Invalid 布尔表）。
	FieldErrors   map[string]string
	HasFieldError bool

	// Form 表单初始值；Echo 本次提交的原始快照（模板一律写
	// formValue(.Echo, "字段名", .Form.X) —— 提交里有这个字段就用提交值，
	// 哪怕空串，没有才用初始值）。
	Form any
	Echo any
}

// SkuOptionResp 订单新建页的 SKU 候选（模板读 .ID / .Label）。
type SkuOptionResp struct {
	ID    string
	Label string
}

// CountryOptionResp 国家/地区下拉的一项（模板读 .Code / .Label / .Selected）。
type CountryOptionResp struct {
	Code     string
	Label    string
	Selected bool
}

// OrderNewItemRowResp 订单新建页的明细行（模板按行渲染候选下拉）。
type OrderNewItemRowResp struct {
	VariantID string
	Quantity  string
}

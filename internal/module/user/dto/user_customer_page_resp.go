package userdto

// user_customer_page_resp.go — 后台客户管理四页的渲染视图（页面 GET 的回执形状）。
//
// 与 user_resp.go 的分工：那一份是接口契约；这一份是页面视图。
// **导出字段名就是模板契约**：internal/templates/admin/user/*.html 里的 `.Field`。
//
// 这一批视图从 gin.H 换过来，动机是模板里的那句实测：Jet 对**缺席的键**求 len()
// 是运行时错误（整页 500，错误信息只有模板行号）。用 gin.H 时「少设一个键」是
// 静默可编译的；用结构体时它至少是个零值（空切片/0），而改名是编译错误。

import "go_wp/pkg/pagination"

// CohortColumnResp 群组表的列头。
//
// 列头在 Go 侧定：Jet 的 `{{range}}` 只能遍历集合，没有「遍历 0..N」这种写法。
// 用**相对月序号**而不是年月：每一行的首单月不同，同一列在各行是不同月份，
// 把某一行的月份当列头会让其它行的格子对不上号。
type CohortColumnResp struct {
	Index int
	Text  string
}

// CustomerCohortPageResp 客户群组页（/admin/customers/cohort）。
type CustomerCohortPageResp struct {
	Title string
	Path  string
	Range any

	ErrKey      string
	ErrFallback string
	HasErr      bool

	// CohortReady 数据读到了没有（nil 结果 = 没读到，与「读到了但为空」是两件事）。
	CohortReady bool
	// Columns / Colspan 列头与合并列数。**三个键无论有没有数据都要有值**：
	// 模板会 len() 它们，而 Jet 对缺席的键求 len 是运行时错误。
	Columns []CohortColumnResp
	Colspan int

	// Cohorts / Customers 是**计数**（群组数 / 客户数），不是列表。
	Cohorts   int64
	Customers int64
	Months    int
	Rows      []any

	// GrowthReady 「客户增长」是否接线。与「有数据」是两件事：未接线时给一句人话，
	// 而不是一片 0（0 会被读成「这段时间一个客户都没来」）。
	GrowthReady         bool
	OrderingCustomers   int64
	NewCustomers        int64
	ReturningCustomers  int64
	Repurchasers        int64
	NewRepurchasers     int64
	RepurchaseRateLabel string

	Pagination pagination.Page
}

// CounterTabResp 客户列表的计数徽章（一个徽章一个链接，URL 由服务端拼好）。
//
// 与 RfmSegmentTabResp 同形但语义不同（一个是状态计数，一个是 RFM 分段）——
// 合成一个会让"改这个页面的徽章"牵动另一个页面。同形的重复在这里是刻意的。
type CounterTabResp struct {
	Key string
	// LabelKey 徽章文案的词条 key（模板用 tr(labelKey, label) 取词）。
	// 字段名 = 模板键；它落在「dto 不带展示字段」名单上，但交的是**词条 key 而非成品句** ——
	// 登记在 pendingDTOPresentation（与 inventory 同类同档）。
	LabelKey string
	Label    string
	Count   int64
	Active  bool
	URL     string
}

// RfmSegmentTabResp 分段筛选徽章（一个徽章一个链接，URL 由服务端拼好）。
//
// 色调由模板按 Key 取（语义档 → 类名的映射在 ui.css），这里只给事实：
// 分段键、计数、当前是否选中、URL（带时间窗口与其他条件，拼串归服务端）。
type RfmSegmentTabResp struct {
	Key string
	// LabelKey 徽章文案的词条 key（模板用 tr(labelKey, label) 取词）。
	// 字段名 = 模板键；它落在「dto 不带展示字段」名单上，但交的是**词条 key 而非成品句** ——
	// 登记在 pendingDTOPresentation（与 inventory 同类同档）。
	LabelKey string
	Label    string
	Count   int64
	Active  bool
	URL     string
}

// CustomerRfmPageResp 客户 RFM 页（/admin/customers/rfm）。
type CustomerRfmPageResp struct {
	Title   string
	Path    string
	Range   any
	Segment string

	ErrKey      string
	ErrFallback string
	HasErr      bool

	Rows  []any
	Page  int
	Limit int
	Total int64

	// RfmReady 数据读到了没有。与「有数据」是两件事：未接线时给一句人话，
	// 而不是三格 0（0 会被读成「这段时间一个客户都没有」）。
	RfmReady bool
	// SegmentTabs 分段徽章（RfmReady 为假时为空切片）。
	SegmentTabs []RfmSegmentTabResp

	Customers int64
	Vip       int64
	Potential int64
	LowValue  int64

	// Pagination 分页事实（模板 admin/partials/pagination.html 按这个键读）。
	Pagination pagination.Page
}

// MembershipTierOptionResp 客户列表的等级下拉项。
//
// ID 是**字符串**：模板要拿它与 FilterTier（字符串）比较，而 Jet 对 int64 与 string
// 的 `==` 不会隐式转换（比出来恒为 false，表现是「选中态永远停在第一项」，页面不报错）。
type MembershipTierOptionResp struct {
	ID   string
	Name string
}

// CustomerListRowResp 客户列表的一行。
//
// 展示名三级回退（展示名 → 昵称 → 登录名）与详情链接都归模板：页面只给三个原值，
// 怎么挑、挑不中显示什么由 Jet 决定（与 block 的行片段口径一致）。
//
// 时间一律给时刻本身（*utils.JSONTime），格式化归模板的 dateTime() —— 预格式化的
// 字符串让模板既没有本地化余地，也分不出「没有这个时刻」与「时刻存在但格式化后为空」。
type CustomerListRowResp struct {
	ID       uint64
	Username string
	Email    string
	// DisplayName 账号自填的展示名（三级回退：DisplayName → Nickname → Username，由模板决定）。
	//
	// 字段名与模板键 `.DisplayName` 逐字一致。它落在「dto 不带展示字段」的禁名单上，
	// 但那条判据针对的是**成品展示值**（预先取好词的句子 / 样式类名）；
	// 这里交的是**作者自填的原始值**，三级回退由模板决定 —— 语义上是数据不是展示。
	// 登记在 pendingDTOPresentation 里（与 inventory 的同类字段同档）。
	DisplayName string
	Nickname    string

	Status              int
	StatusLabelKey           string
	StatusLabelFallback      string
	EmailVerified       bool
	EmailVerifiedKey      string
	EmailVerifiedFallback string

	RegisteredAt      any
	RegisterIP        string
	RegisterLocation  string
	LastLoginTime     any
	LastLoginIP       string
	LastLoginLocation string
	Locked            bool
	LockedUntil       any
	LoginFailureCount int

	// Actionable 只有「正常 ↔ 已停用」两个方向。
	//
	// 待激活的账号刻意不给按钮：它登不上去（status != active 一律拒绝登录），
	// 停用它只会让客户点验证链接时得到「链接失效」—— 既没解决问题，
	// 又让客户来问「为什么我的链接坏了」。
	Actionable          bool
	NextStatus          int
	StatusActionKey string
	StatusActionLabel    string

	// PendingHint / LockHint 说明文案的词条事实（空 key = 没有这条提示）。
	PendingHintKey string
	PendingHintFallback    string
	LockHintKey    string
	LockHintFallback       string
}

// CustomersListPageResp 客户列表整页（/admin/customers）。
//
// 字段名与模板键逐字一致（customers.html 的 `.Field`）。零值可用：失败档渲染同一张模板。
type CustomersListPageResp struct {
	Title    string
	Menu     string
	Rows     []CustomerListRowResp
	Total    int64
	Page     int
	Limit    int
	Counters CustomerCounters

	// CounterTabs 计数徽章（状态 / 邮箱 / 锁定三个维度的筛选控件，信息与操作合一）。
	CounterTabs []CounterTabResp
	// TierOptions 等级下拉（空切片而不是 nil：模板会 range 它）。
	TierOptions []MembershipTierOptionResp

	// 筛选条件的回显（模板逐项判「是否生效」）。
	FilterActive      bool
	FilterStatus      int
	FilterVerified    int
	FilterLocked      bool
	FilterTier        string
	FilterMinOrders   string
	FilterSegment     string
	FilterRfm         string
	FilterSegmentFrom string
	FilterSegmentTo   string
	FilterFrom        string
	FilterTo          string
	FilterKeyword     string

	// ErrKey / ErrFallback 归口提示的词条事实（空 key = 无错误）。
	ErrKey      string
	ErrFallback string
	HasErr      bool

	CapabilityMissing bool
	BackPath          string

	Pagination pagination.Page
}

// Package analyticsdto analytics 模块请求/响应结构。
package analyticsdto

// CollectReq 一次页面浏览上报。
//
// 公开端点（/analytics/collect）的输入，**每一个字段都不可信**：访客可以手工构造。
// 因此 service 对每个字段做形状归一化与截断，形状不合法一律静默丢弃
// （打点端点不该因为坏输入给访客任何反馈，也不该把坏数据写进统计）。
type CollectReq struct {
	// ProjectID 站点工程 id（构建期烘进产物的 __skyTrack 配置）。
	ProjectID string
	// Path 页面路径（只取 pathname，服务端再截断查询串与长度）。
	Path string
	// Lang 页面语言（构建期烘进产物）。
	Lang string
	// Referrer 来源域（**只发域名**，不发完整 URL）。
	Referrer string
	// Session 会话的匿名派生标识（如 s1712345678）——服务端 hash 后才落库。
	Session string
	// Visitor 访客的匿名派生标识（如 v1700000000-3）——服务端 hash 后才落库。
	Visitor string
	// IP 客户端 IP（服务端从连接取；**只落 hash，不存明文**）。
	IP string
	// UserAgent 客户端 UA（服务端从请求头取；只落粗粒度分类）。
	UserAgent string
}

// SummaryReq 访问统计查询（后台只读）。
type SummaryReq struct {
	// ProjectID 站点工程 id（必填）。
	ProjectID string `form:"projectId" json:"projectId"`
	// From 起始日期（YYYY-MM-DD，含当天）；空 = 默认最近 30 天。
	From string `form:"from" json:"from"`
	// To 结束日期（YYYY-MM-DD，含当天）；空 = 今天。
	To string `form:"to" json:"to"`
	// PathPage 按路径聚合的分页页码（1 起）。
	PathPage int `form:"pathPage" json:"pathPage"`
	// PathLimit 按路径聚合的每页条数（默认 20，上限 200）。
	PathLimit int `form:"pathLimit" json:"pathLimit"`
	// PathAfterViews / PathAfter 路径排行的游标（keyset 分页，审计 IDX-010）：
	// 传上一页最后一行的 (views, path)，返回它之后的一页。
	//
	// 与 PathPage 的关系：给了游标就走游标（成本与页码无关，深分页不再随页码变慢）；
	// 没给则按 PathPage 用 offset —— 第一页 offset=0 本身就是最优路径，
	// 后台页面因此可以只翻「下一页」时改用游标，无需整体改造。
	PathAfterViews int64  `form:"pathAfterViews" json:"pathAfterViews"`
	PathAfter      string `form:"pathAfter" json:"pathAfter"`
	// RankLimit 维度排行（来源域 / 设备分类 / 语言）各取前多少条（默认 20，上限 200）。
	//
	// 三组维度**不做分页**：它们的取值域是天然收敛的（设备分类最多 5 种、语言十几、
	// 来源域远少于路径数），Top-N 已经覆盖运营要看的部分；给三个榜各配一套游标
	// 只会让调用方多维护三份翻页状态，换不到任何东西。
	RankLimit int `form:"rankLimit" json:"rankLimit"`
	// Granularity 逐点聚合的粒度：空 / "day" 按天，"hour" 按小时。
	//
	// 按小时时 Daily 里的 Day 变成 YYYY-MM-DDTHH:00（见 service 的 hourLayout），
	// 且**强制走明细表** —— 预聚合表 page_views_daily 的最小粒度就是天。
	// 未知取值按天处理（与「空 = 默认」同一收敛策略）。
	Granularity string `form:"granularity" json:"granularity"`
}

// 统计取数来源（响应回显，便于确认「这次数字是明细还是预聚合给的」）。
const (
	// SourceDetail 明细表 page_views（窗口含今天时的唯一选择）。
	SourceDetail = "detail"
	// SourceSummary 按天预聚合表 page_views_daily（窗口完全在过去时）。
	SourceSummary = "summary"
)

// DailyCount 某一天的浏览数与独立访客数（独立访客按匿名 visitor hash 去重）。
type DailyCount struct {
	// Day 日期（YYYY-MM-DD，UTC 日界；产物由全球访客访问，用 UTC 避免时区歧义）。
	Day string `json:"day"`
	// Views 该天浏览数（PV）。
	Views int64 `json:"views"`
	// Visitors 该天独立访客数（UV）。
	Visitors int64 `json:"visitors"`
}

// PathCount 某个路径的浏览数与独立访客数。
type PathCount struct {
	Path     string `json:"path"`
	Views    int64  `json:"views"`
	Visitors int64  `json:"visitors"`
}

// RankCount 某个维度取值（来源域 / 设备分类 / 语言）的浏览数与独立访客数。
//
// 三组排行共用同一个形状：Value 的语义由它所在的数组决定（Referrers 里是域名、
// UAClasses 里是分类、Langs 里是语言码）。三个近乎相同的结构体只会让 DTO
// 与模板各多两份重复，而它们的字段名本来就一模一样。
type RankCount struct {
	// Value 维度取值。**空串是合法取值**：它代表「没有来源 / UA 缺失 / 没上报语言」
	//（见 model.CountByDimension 的说明），展示层负责渲染成占位文案。
	Value    string `json:"value"`
	Views    int64  `json:"views"`
	Visitors int64  `json:"visitors"`
}

// SummaryResp 访问统计汇总。
type SummaryResp struct {
	ProjectID string `json:"projectId"`
	// From / To 实际生效的查询窗口（YYYY-MM-DD，均为**含当天**）。
	From string `json:"from"`
	To   string `json:"to"`
	// Total 窗口内总浏览数（PV）。
	Total int64 `json:"total"`
	// Visitors 窗口内独立访客数（UV）。
	Visitors int64 `json:"visitors"`
	// Daily 按天聚合（升序，只含有点击的天）。
	Daily []DailyCount `json:"daily"`
	// Paths 按路径聚合（浏览数降序），受 PathPage / PathLimit 分页约束。
	Paths []PathCount `json:"paths"`
	// PathTotal 路径聚合的总条数（分页用）。
	PathTotal int64 `json:"pathTotal"`
	// PathPage / PathLimit 本次分页参数（回显用）。
	PathPage  int `json:"pathPage"`
	PathLimit int `json:"pathLimit"`
	// Referrers / UAClasses / Langs 来源域 / 设备分类 / 语言的排行
	// （浏览数降序，并列时按取值升序；各取 RankLimit 条）。
	Referrers []RankCount `json:"referrers"`
	UAClasses []RankCount `json:"uaClasses"`
	Langs     []RankCount `json:"langs"`
	// RankLimit 本次三组排行的条数上限（回显用）。
	RankLimit int `json:"rankLimit"`
	// BreakdownSource 三组维度排行的取数来源，**恒为 detail**。
	//
	// 与 Source 分开回显而不是复用它：Source 讲的是 Total / Daily / Paths 的来源，
	// 而维度排行固定读明细（理由见 service.Summary 的形态选择注释）。
	// 让调用方从 Source 反推维度排行的来源，会在「窗口完全在过去」的请求上
	// 得到相反的结论 —— 那种回显错误比不回显更难发现。
	BreakdownSource string `json:"breakdownSource"`
	// Source 本次统计的取数来源（detail / summary），见上方常量。
	Source string `json:"source"`
	// PathNextAfterViews / PathNextAfter 下一页游标（本页最后一行）；
	// Paths 为空时为空 —— 「没有下一页」与「下一页刚好从空开始」是两回事，
	// 用空值统一表示前者，客户端据此收起翻页按钮。
	PathNextAfterViews int64  `json:"pathNextAfterViews"`
	PathNextAfter      string `json:"pathNextAfter"`
}

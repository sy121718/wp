package analyticsdto

// Package analyticsdto analytics 模块请求/响应结构。

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

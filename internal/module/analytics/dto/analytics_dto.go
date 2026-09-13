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
}

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
}

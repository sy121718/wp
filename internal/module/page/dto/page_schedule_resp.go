package pagedto

import "go_wp/pkg/utils"

// ScheduleItemResp 一条排定的投影（后台面板与接口共用）。
type ScheduleItemResp struct {
	ID       int64  `json:"id"`
	PageID   string `json:"pageId"`
	Lang     string `json:"lang"`
	Action   string `json:"action"`
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
	// ScheduledAt 到点时刻（绝对时刻，RFC3339 到秒）。
	ScheduledAt utils.JSONTime `json:"scheduledAt"`
	// ScheduledAtLocal 站点时区下的 "2006-01-02T15:04"（后台 datetime-local 回显用）。
	ScheduledAtLocal string `json:"scheduledAtLocal"`
	// ArtifactID 排定时冻结的产物行 id（publish 时非空）。
	ArtifactID string `json:"artifactId,omitempty"`
	// DraftVersion 排定当时的草稿版本（到点比对的前置判据）。
	DraftVersion int64 `json:"draftVersion"`
	// RedirectPath 下线时的 301 落点（空 = 直接下线）。
	RedirectPath string `json:"redirectPath,omitempty"`
	// LastError 最近一次失败的**业务 key**（不是原文；原文只进日志）。
	LastError string `json:"lastError,omitempty"`
	// LastErrorText 上者按当前语言取的文案。
	//
	// 由**读侧**（handler，拿得到请求语言）填：service 不翻译（它没有语言上下文，
	// 硬翻会把中英混排写进接口响应）。给页面投影用，JSON 调用方可以忽略它。
	LastErrorText string         `json:"lastErrorText,omitempty"`
	CreateTime    utils.JSONTime `json:"createTime"`
	UpdateTime    utils.JSONTime `json:"updateTime"`
}

// ScheduleListResp 排定列表。
type ScheduleListResp struct {
	PageID string             `json:"pageId"`
	Items  []ScheduleItemResp `json:"items"`
}

// SchedulePageSummaryResp 单个页面的排定投影（后台列表页的行内徽标用）。
//
// 为什么不是「一个页面一份排定列表」：列表页一屏几十行，逐页问一次会把一个页面渲染
// 变成几十次查询；这条投影按 (status IN pending/running/failed) 一次取回全部命中行再分组。
// 终态里的 done / canceled 不出现在这里 —— 列表页要回答的是「这页是不是排了队、
// 有没有排失败」，不是排定历史（那是面板的事）。
type SchedulePageSummaryResp struct {
	PageID string `json:"pageId"`
	// Pending 待执行 / 执行中的排定（同键唯一索引保证每页每语言每动作至多一条，
	// 多语言 / 多动作时这里取**最近创建**的一条）。
	Pending *ScheduleItemResp `json:"pending,omitempty"`
	// Failed 最近一次失败的排定（终态）：后台据此显示「排定失败」并把原因摆出来 ——
	// 决策 2 要求的「到点硬失败」必须**在后台可见**，否则与静默不生效没有区别。
	Failed *ScheduleItemResp `json:"failed,omitempty"`
}

// ScheduleRunResp 一次到点扫描的执行统计（调度器与手动触发共用）。
type ScheduleRunResp struct {
	// Claimed 本轮认领的排定条数。
	Claimed int `json:"claimed"`
	// Applied 执行成功的条数。
	Applied int `json:"applied"`
	// Failed 不可重试失败（已置 failed）的条数。
	Failed int `json:"failed"`
	// Retried 退回待执行、等待下一轮的条数。
	Retried int `json:"retried"`
	// Reclaimed 本轮回收的超时租约条数。
	Reclaimed int64 `json:"reclaimed"`
}

package pagedto

import "go_wp/pkg/utils"

// page_schedule_dto.go — 定时上下线（PIPE-7）的请求 / 响应结构。
//
// 时间口径（pkg/sitetz 的文件头）：**存储与比较一律绝对时刻**（列是 timestamptz，
// 服务层用 UTC 计算）；**面向人的输入输出按站点时区理解**。
//
//   · 入参 ScheduledAt 用 string 而不是 utils.JSONTime —— JSONTime 的 Unmarshal 走
//     time.Local 解析「无时区字面量」，而这里的口径是**站点时区**（部署用
//     GO_WP_SITE_TIMEZONE 固定时区后，两者会分叉：同一串 "2026-09-30 10:00"
//     一个按服务器本地时区、一个按站点时区，相差若干小时且不报错）。
//     解析在 service 层单点完成（sitetz.ParseDateTime）。
//   · 出参用 utils.JSONTime（对外只到秒，全库 dto 统一口径），另附 *Local 形态
//     供后台表单回显 —— 表单控件（datetime-local）要的是站点时区的字面量。

// ScheduleSetReq 排定一次「到点上线 / 到点下线」。
type ScheduleSetReq struct {
	// PageID 必填（页面 id）。
	PageID string `json:"pageId"`
	// Lang 目标语言（空 = 站点默认语言，与 Build / Publish 同一口径）。
	Lang string `json:"lang"`
	// Action publish / offline（见 page/model 的 ScheduleAction* 常量）。
	Action string `json:"action"`
	// ScheduledAt 到点时刻，按**站点时区**解释（支持 "2006-01-02 15:04[:05]"、
	// "2006-01-02T15:04[:05]" 与带偏移量的 RFC3339）。必须晚于当前时刻。
	ScheduledAt string `json:"scheduledAt"`
	// RedirectPath 仅下线用：旧路径 301 到这里（空 = 直接下线，访问面 404）。
	RedirectPath string `json:"redirectPath"`
	// CreateBy 排定发起人（= sys_admin.id，bigint，与全库 create_by 同型），
	// 由 **handler** 用 shell.CurrentUserID 填（0 = 未登录 / 系统排定，落库为 NULL）。
	//
	// `json:"-"` 是硬要求：它不从请求体读。让客户端自报发起人等于把审计字段交给请求方
	// 决定，而 page_schedules.create_by 的用途正是「事后回答谁排的」。
	CreateBy int64 `json:"-"`
}

// ScheduleCancelReq 取消一条尚未执行的排定。
type ScheduleCancelReq struct {
	// PageID 必填（归属校验：不给页面 id 就能按排定 id 取消别人的排定）。
	PageID string `json:"pageId"`
	// ID 排定 id（page_schedules.id，纯内部流水）。
	ID int64 `json:"id"`
}

// ScheduleListReq 列出某个页面的排定。
type ScheduleListReq struct {
	// PageID 必填。
	PageID string `form:"pageId" json:"pageId"`
	// Limit 返回条数上限（<=0 时取默认值）。
	Limit int `form:"limit" json:"limit"`
}

// ScheduleItem 一条排定的投影（后台面板与接口共用）。
type ScheduleItem struct {
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
	PageID string         `json:"pageId"`
	Items  []ScheduleItem `json:"items"`
}

// SchedulePageSummary 单个页面的排定投影（后台列表页的行内徽标用）。
//
// 为什么不是「一个页面一份排定列表」：列表页一屏几十行，逐页问一次会把一个页面渲染
// 变成几十次查询；这条投影按 (status IN pending/running/failed) 一次取回全部命中行再分组。
// 终态里的 done / canceled 不出现在这里 —— 列表页要回答的是「这页是不是排了队、
// 有没有排失败」，不是排定历史（那是面板的事）。
type SchedulePageSummary struct {
	PageID string `json:"pageId"`
	// Pending 待执行 / 执行中的排定（同键唯一索引保证每页每语言每动作至多一条，
	// 多语言 / 多动作时这里取**最近创建**的一条）。
	Pending *ScheduleItem `json:"pending,omitempty"`
	// Failed 最近一次失败的排定（终态）：后台据此显示「排定失败」并把原因摆出来 ——
	// 决策 2 要求的「到点硬失败」必须**在后台可见**，否则与静默不生效没有区别。
	Failed *ScheduleItem `json:"failed,omitempty"`
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

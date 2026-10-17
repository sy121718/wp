package pagedto

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
	// 由 **handler** 用 builtin.CurrentUserID 填（0 = 未登录 / 系统排定，落库为 NULL）。
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

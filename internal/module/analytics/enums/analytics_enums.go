// Package analyticsenums 统一管理 analytics 模块响应消息。
package analyticsenums

const (
	// ErrInvalidParam 查询参数无效（工程未指定 / 时间范围不合法）。
	ErrInvalidParam = "ErrInvalidParam" // 请求参数无效
	// ErrInvalidRange 时间范围不合法（起止颠倒或超过上限天数）。
	ErrInvalidRange = "ErrInvalidRange" // 时间范围不合法：起始日期不能晚于结束日期，且跨度不超过 366 天
	// ErrAnalyticsInternal 统计服务内部错误（基础设施故障兜底，不向客户端泄漏内部细节）。
	ErrAnalyticsInternal = "ErrAnalyticsInternal" // 统计服务内部错误
)

const (
	// MsgCollectAccepted 打点已受理（公开端点实际返回 204 空响应，此文案供日志与测试引用）。
	MsgCollectAccepted = "MsgCollectAccepted" // 打点已受理
)

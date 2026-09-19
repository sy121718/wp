// Package pubenums 统一管理 publication 模块业务消息。
package pubenums

const (
	ErrRouteOccupied  = "ErrRouteOccupied"  // 目标路径已被其他页面占用
	ErrRouteNotFound  = "ErrRouteNotFound"  // 路径占用不存在
	ErrReceiptPending = "ErrReceiptPending" // 存在未完成的发布回执，请先恢复或标记失败
	// ErrInvalidParam 请求本身不合法（nil 请求、缺少必要字段等），与路径/占用无关。
	ErrInvalidParam = "ErrInvalidParam" // 请求参数无效
	// ErrRouteActiveRename 对已激活（线上）路径执行草稿改名：非法状态转移，
	// 应走页面改 URL（UpdateURL）流程而非直接改 active 行。
	ErrRouteActiveRename = "ErrRouteActiveRename" // 已激活的线上路径不能直接改名，请通过页面改 URL 流程操作
	// ErrAuditFailed SEO 体检本身失败（读不到产物目录、解析异常等）。
	//
	// 存在的理由同 order / user 模块的 ErrInternal：RunSEOAudit 返回的可能是基础设施
	// 错误的原文（带路径甚至 SQL 片段），直接铺给后台页面违反 CQ-009；这里给一条
	// 可翻译的归口文案，原文进日志。
	ErrAuditFailed = "ErrAuditFailed" // SEO 体检失败，请查看服务端日志
)

const (
	MsgPublished   = "MsgPublished"   // 发布成功
	MsgRolledBack  = "MsgRolledBack"  // 回滚成功
	MsgDeactivated = "MsgDeactivated" // 取消激活成功
)

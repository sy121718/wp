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
	// ErrInternal 未归类的系统错误对外统一文案（AGENTS.md「错误文案三件套」的②）。
	//
	// 与 adminenums.ErrInternal **同 key**（值都是 "ErrInternal"）：这是同一句通用归口文案
	// ——「操作失败，请稍后重试」对哪个模块都一样，共用词条比每个模块各造一条好。
	// 它的用途只有一个：把基础设施错误（SQL 原文 / 约束名 / 路径）挡在响应之外，
	// 原文只进结构化日志（形态参照 admin_err.go 与 order 模块的 orderFacingText）。
	ErrInternal = "ErrInternal" // 操作失败，请稍后重试

)

const (
	MsgPublished   = "MsgPublished"   // 发布成功
	MsgRolledBack  = "MsgRolledBack"  // 回滚成功
	MsgDeactivated = "MsgDeactivated" // 取消激活成功
)

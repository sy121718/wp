// Package pubenums 统一管理 publication 模块业务消息。
package pubenums

const (
	ErrRouteOccupied  = "目标路径已被其他页面占用"
	ErrRouteNotFound  = "路径占用不存在"
	ErrReceiptPending = "存在未完成的发布回执，请先恢复或标记失败"
	// ErrInvalidParam 请求本身不合法（nil 请求、缺少必要字段等），与路径/占用无关。
	ErrInvalidParam = "请求参数无效"
	// ErrRouteActiveRename 对已激活（线上）路径执行草稿改名：非法状态转移，
	// 应走页面改 URL（UpdateURL）流程而非直接改 active 行。
	ErrRouteActiveRename = "已激活的线上路径不能直接改名，请通过页面改 URL 流程操作"
)

const (
	MsgPublished   = "发布成功"
	MsgRolledBack  = "回滚成功"
	MsgDeactivated = "取消激活成功"
)

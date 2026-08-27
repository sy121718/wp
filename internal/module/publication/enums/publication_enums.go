// Package pubenums 统一管理 publication 模块业务消息。
package pubenums

const (
	ErrRouteOccupied  = "目标路径已被其他页面占用"
	ErrRouteNotFound  = "路径占用不存在"
	ErrReceiptPending = "存在未完成的发布回执，请先恢复或标记失败"
)

const (
	MsgPublished   = "发布成功"
	MsgRolledBack  = "回滚成功"
	MsgDeactivated = "取消激活成功"
)

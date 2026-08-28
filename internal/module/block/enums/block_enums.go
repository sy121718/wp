// Package blockenums 统一管理 block 模块响应消息与错误消息。
package blockenums

const (
	MsgBlockTitle = "全局块"

	ErrBlockNotFound     = "全局块不存在"
	ErrBlockNameRequired = "块名称不能为空"
	ErrBlockInvalidDoc   = "块文档不合法"
	ErrBlockInvalidKind  = "块类型不合法"
)

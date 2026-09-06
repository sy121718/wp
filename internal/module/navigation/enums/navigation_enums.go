// Package navigationenums 统一管理 navigation 模块响应消息（0-C）。
package navigationenums

// 成功消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess = "导航项创建成功"
	MsgUpdateSuccess = "导航项更新成功"
	MsgListSuccess   = "导航列表获取成功"
	MsgDetailSuccess = "导航详情获取成功"
	MsgDeleteSuccess = "导航项删除成功"
)

// 错误消息。
const (
	ErrInvalidParam = "参数错误"
	ErrNotFound     = "导航项不存在"
	ErrInvalidKind  = "非法的导航类型，仅支持 header 或 footer"
	ErrPathTaken    = "同工程同类型下已存在相同路径的导航项"
)

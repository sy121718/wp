// Package contentenums content 模块响应消息。
package contentenums

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess = "内容创建成功"
	MsgUpdateSuccess = "内容更新成功"
	MsgListSuccess   = "内容列表获取成功"
	MsgDetailSuccess = "内容详情获取成功"
	MsgDeleteSuccess = "内容已删除"

	ErrInvalidParam = "参数错误"
	ErrNotFound     = "内容不存在"
	ErrInvalidType  = "不支持的内容类型"
	ErrInvalidField = "内容字段不在白名单"
	ErrSlugTaken    = "同类型下 slug 已存在"
	ErrDataInvalid  = "内容数据格式非法"
)

// Package blueprintenums blueprint 模块响应消息。
package blueprintenums

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess  = "Blueprint 创建成功"
	MsgUpdateSuccess  = "Blueprint 更新成功"
	MsgPublishSuccess = "Blueprint 发布成功"
	MsgListSuccess    = "Blueprint 列表获取成功"
	MsgDetailSuccess  = "Blueprint 详情获取成功"
	MsgDeleteSuccess  = "Blueprint 已删除"

	ErrInvalidParam = "参数错误"
	ErrNotFound     = "Blueprint 不存在"
	ErrInvalidKind  = "不支持的页面类型"
	ErrDataInvalid  = "Blueprint 文档格式非法"
)

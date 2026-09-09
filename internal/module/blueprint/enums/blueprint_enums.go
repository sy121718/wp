// Package blueprintenums blueprint 模块响应消息。
package blueprintenums

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess  = "MsgCreateSuccess"  // Blueprint 创建成功
	MsgUpdateSuccess  = "MsgUpdateSuccess"  // Blueprint 更新成功
	MsgPublishSuccess = "MsgPublishSuccess" // Blueprint 发布成功
	MsgListSuccess    = "MsgListSuccess"    // Blueprint 列表获取成功
	MsgDetailSuccess  = "MsgDetailSuccess"  // Blueprint 详情获取成功
	MsgDeleteSuccess  = "MsgDeleteSuccess"  // Blueprint 已删除

	ErrInvalidParam = "ErrInvalidParam" // 参数错误
	ErrNotFound     = "ErrNotFound"     // Blueprint 不存在
	ErrInvalidKind  = "ErrInvalidKind"  // 不支持的页面类型
	ErrDataInvalid  = "ErrDataInvalid"  // Blueprint 文档格式非法
)

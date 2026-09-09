// Package contenttemplateenums contenttemplate 模块响应消息。
package contenttemplateenums

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess = "MsgCreateSuccess" // 模板创建成功
	MsgUpdateSuccess = "MsgUpdateSuccess" // 模板更新成功
	MsgListSuccess   = "MsgListSuccess"   // 模板列表获取成功
	MsgDetailSuccess = "MsgDetailSuccess" // 模板详情获取成功

	ErrInvalidParam = "ErrInvalidParam" // 参数错误
	ErrNotFound     = "ErrNotFound"     // 模板不存在
	ErrInvalidType  = "ErrInvalidType"  // 不支持的内容类型
	ErrDataInvalid  = "ErrDataInvalid"  // 模板文档格式非法
)

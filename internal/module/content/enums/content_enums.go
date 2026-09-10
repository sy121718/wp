// Package contentenums content 模块响应消息。
package contentenums

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess = "MsgCreateSuccess" // 内容创建成功
	MsgUpdateSuccess = "MsgUpdateSuccess" // 内容更新成功
	MsgListSuccess   = "MsgListSuccess"   // 内容列表获取成功
	MsgDetailSuccess = "MsgDetailSuccess" // 内容详情获取成功
	MsgDeleteSuccess = "MsgDeleteSuccess" // 内容已删除
	// MsgCollectionsSuccess 集合源元数据获取成功。
	MsgCollectionsSuccess = "MsgCollectionsSuccess"

	ErrInvalidParam = "ErrInvalidParam" // 参数错误
	ErrNotFound     = "ErrNotFound"     // 内容不存在
	ErrInvalidType  = "ErrInvalidType"  // 不支持的内容类型
	ErrInvalidField = "ErrInvalidField" // 内容字段不在白名单
	ErrSlugTaken    = "ErrSlugTaken"    // 同类型下 slug 已存在
	ErrDataInvalid  = "ErrDataInvalid"  // 内容数据格式非法
	// ErrCollectionUnsupported 当前内容服务未实现集合元数据契约。
	ErrCollectionUnsupported = "ErrCollectionUnsupported"
)

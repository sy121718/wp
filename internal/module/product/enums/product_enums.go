// Package productenums product 模块响应消息。
package productenums

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess = "MsgCreateSuccess"
	MsgUpdateSuccess = "MsgUpdateSuccess"
	MsgDeleteSuccess = "MsgDeleteSuccess"
	MsgListSuccess   = "MsgListSuccess"
	MsgDetailSuccess = "MsgDetailSuccess"

	ErrInvalidParam = "ErrInvalidParam" // 参数错误
	ErrNotFound     = "ErrNotFound"     // 商品或变体不存在
	ErrSlugTaken    = "ErrSlugTaken"    // 同工程下 slug 已占用
	ErrSkuTaken     = "ErrSkuTaken"     // 同商品下 SKU 编码已占用
	ErrNameRequired = "ErrNameRequired" // 商品名称必填
)

// 商品状态。
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusArchived  = "archived"
)

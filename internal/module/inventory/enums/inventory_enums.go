// Package inventoryenums inventory 模块响应消息（issue #15）。
//
// 与 product 模块同形：未接 i18n 前 ErrXxx / MsgXxx 是消息键常量，
// handler 与 service 一律引用本包，不硬编码文案。
package inventoryenums

// 响应消息。
const (
	MsgCreateSuccess = "MsgCreateSuccess"
	MsgUpdateSuccess = "MsgUpdateSuccess"
	MsgDeleteSuccess = "MsgDeleteSuccess"
	MsgListSuccess   = "MsgListSuccess"
	MsgDetailSuccess = "MsgDetailSuccess"
)

// 错误消息。
const (
	ErrInvalidParam = "ErrInvalidParam" // 参数错误

	// —— 仓库（issue #15 验收 1）——
	ErrWarehouseNotFound        = "ErrWarehouseNotFound"        // 仓库不存在
	ErrWarehouseNameRequired    = "ErrWarehouseNameRequired"    // 仓库名称必填
	ErrWarehouseCodeRequired    = "ErrWarehouseCodeRequired"    // 仓库短码必填
	ErrWarehouseCodeInvalid     = "ErrWarehouseCodeInvalid"     // 短码不是字母数字（SKU 编码前缀只允许 A-Z 0-9）
	ErrWarehouseCodeTaken       = "ErrWarehouseCodeTaken"       // 同工程下短码已占用
	ErrWarehouseStatusInvalid   = "ErrWarehouseStatusInvalid"   // 状态不是 active / disabled
	ErrWarehouseIsDefault       = "ErrWarehouseIsDefault"       // 默认仓不能删除，也不能取消默认 / 停用
	ErrWarehouseHasStock        = "ErrWarehouseHasStock"        // 仓内仍有非零库存，不能删除
	ErrWarehouseDisabled        = "ErrWarehouseDisabled"        // 已停用的仓库不能作为归属仓
	ErrWarehouseProjectMismatch = "ErrWarehouseProjectMismatch" // 仓库不属于该工程
	// ErrWarehouseDefaultMissing 「未指定仓库」时工程内没有默认仓可兜底 ——
	// 兜底铁律不是「随便挑一个仓」，缺默认仓即数据缺陷，必须显式暴露。
	ErrWarehouseDefaultMissing = "ErrWarehouseDefaultMissing"

	// —— 库存记录（issue #15 验收 2/3）——
	ErrStockNotFound        = "ErrStockNotFound"        // 库存记录不存在
	ErrStockVariantRequired = "ErrStockVariantRequired" // 生成库存记录必须给出变体
	ErrStockWarehouseNeeded = "ErrStockWarehouseNeeded" // 生成库存记录必须给出仓库（或可兜底的默认仓）
)

// 仓库状态取值（写入即校验，不接受自由文本）。
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

package webhookenums

// webhook_enums.go — webhook 模块的事件类型 / 状态枚举。

// 事件类型常量（预定义常用事件；event_type 为自由字符串，便于业务扩展）。
const (
	EventTypeOrderPaid       = "order.paid"
	EventTypeOrderRefunded   = "order.refunded"
	EventTypeProductUpdated  = "product.updated"
	EventTypeContentPublishd = "content.published"
)

// 端点启用状态。
const (
	EndpointStatusDisabled = 0
	EndpointStatusEnabled  = 1
)

// 投递状态。
const (
	DeliveryStatusPending   = "pending"
	DeliveryStatusDelivered = "delivered"
	DeliveryStatusFailed    = "failed"
)

// 响应消息（handle 与 service 不硬编码文案，一律取这里）。
const (
	MsgSaveSuccess   = "保存成功"
	MsgDeleteSuccess = "删除成功"
	MsgStatusSuccess = "状态已更新"
	MsgRetryQueued   = "已重新入队"

	ErrInvalidParam       = "参数错误"
	ErrEndpointNotFound   = "端点不存在"
	ErrEventTypeRequired  = "事件类型不能为空"
	ErrTargetURLRequired  = "目标地址不能为空"
	ErrSecretRequired     = "签名密钥不能为空"
	ErrDeliveryNotFound   = "投递记录不存在"
	ErrDeliveryNotFailed  = "只有失败的投递才能重投"
	ErrDeliveryNotPending = "投递已在进行中或已完成"
)

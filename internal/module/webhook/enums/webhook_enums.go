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

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
//
// 四态而不是「待投 + 两个终态」：**投递是要花时间的**（出站 HTTP，客户端超时 15s），
// 「还没投」与「正在投」必须分开 —— 否则两个 worker 可以同时从 pending 出发，
// 同一个外部系统会收到两次同样的签名请求，而两边的事后落定只有一个能成功。
// delivering 是**抢占态**（认领 + 租约），语义与租约取值见 model 的 ClaimDelivery
// 与 service 的 deliverLease。
const (
	DeliveryStatusPending    = "pending"
	DeliveryStatusDelivering = "delivering"
	DeliveryStatusDelivered  = "delivered"
	DeliveryStatusFailed     = "failed"
)

// 响应消息（handle 与 service 不硬编码文案，一律取这里）。
//
// 取值是 **i18n key** 而不是中文原文（与 mail / product 等模块一致）：
//
//	· pkg/response.translate 按请求语言查 sys_i18n，未命中原样返回 key（可见的降级）；
//	· 更重要的是 pkg/response 的 ErrorAuto：它按「值是 key 形态」判定业务错误 ——
//	  中文原文会被判成内部错误而返回 500 + 通用文案（error_auto_test 逐个校验全仓 enums）。
//
// 词条见迁移 216。新增常量时同步补词条，否则前端看到的是 key。
const (
	MsgSaveSuccess   = "webhook.msg.saveSuccess"
	MsgDeleteSuccess = "webhook.msg.deleteSuccess"
	MsgStatusSuccess = "webhook.msg.statusSuccess"
	MsgRetryQueued   = "webhook.msg.retryQueued"

	ErrInvalidParam      = "webhook.err.invalidParam"
	ErrEndpointNotFound  = "webhook.err.endpointNotFound"
	ErrEventTypeRequired = "webhook.err.eventTypeRequired"
	ErrTargetURLRequired = "webhook.err.targetUrlRequired"
	ErrSecretRequired    = "webhook.err.secretRequired"
	// 出站目标（SSRF 防护，SEC-015）：校验失败是**用户可纠正的输入问题**，
	// 必须是业务错误（key 形态）—— 用中文文案会被 ErrorAuto 判成内部错误，
	// 用户填了个内网地址却看到「服务器内部错误」。
	ErrURLMalformed         = "webhook.err.urlMalformed"
	ErrURLSchemeUnsupported = "webhook.err.urlSchemeUnsupported"
	ErrURLHostMissing       = "webhook.err.urlHostMissing"
	ErrURLUnresolvable      = "webhook.err.urlUnresolvable"
	ErrURLDenied            = "webhook.err.urlDenied"

	ErrDeliveryNotFound   = "webhook.err.deliveryNotFound"
	ErrDeliveryNotFailed  = "webhook.err.deliveryNotFailed"
	ErrDeliveryNotPending = "webhook.err.deliveryNotPending"
)

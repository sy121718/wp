package webhookenums

import "go_wp/pkg/facing"

const (
	EventTypeOrderPaid       = "order.paid"
	EventTypeOrderRefunded   = "order.refunded"
	EventTypeProductUpdated  = "product.updated"
	EventTypeContentPublishd = "content.published"
)

const (
	EndpointStatusDisabled = 0
	EndpointStatusEnabled  = 1
)

const (
	DeliveryStatusPending    = "pending"
	DeliveryStatusDelivering = "delivering"
	DeliveryStatusDelivered  = "delivered"
	DeliveryStatusFailed     = "failed"
)

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

	ErrURLMalformed         = "webhook.err.urlMalformed"
	ErrURLSchemeUnsupported = "webhook.err.urlSchemeUnsupported"
	ErrURLHostMissing       = "webhook.err.urlHostMissing"
	ErrURLUnresolvable      = "webhook.err.urlUnresolvable"
	ErrURLDenied            = "webhook.err.urlDenied"

	ErrDeliveryNotFound   = "webhook.err.deliveryNotFound"
	ErrDeliveryNotFailed  = "webhook.err.deliveryNotFailed"
	ErrDeliveryNotPending = "webhook.err.deliveryNotPending"
)

var errFallbacks = map[string]string{
	ErrInvalidParam:         "参数错误",
	ErrEndpointNotFound:     "端点不存在",
	ErrEventTypeRequired:    "事件类型不能为空",
	ErrTargetURLRequired:    "目标地址不能为空",
	ErrSecretRequired:       "签名密钥不能为空",
	ErrURLMalformed:         "目标地址格式不正确",
	ErrURLSchemeUnsupported: "目标地址只支持 http 或 https",
	ErrURLHostMissing:       "目标地址缺少主机名",
	ErrURLUnresolvable:      "目标地址的域名无法解析",
	ErrURLDenied:            "目标地址指向内网或保留地址，已拒绝",
	ErrDeliveryNotFound:     "投递记录不存在",
	ErrDeliveryNotFailed:    "只有失败的投递才能重投",
	ErrDeliveryNotPending:   "投递已在进行中或已完成",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }

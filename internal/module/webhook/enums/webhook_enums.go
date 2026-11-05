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

func Err(key string) error { return facing.New(key) }


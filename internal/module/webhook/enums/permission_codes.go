package webhookenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	WebhookDeliveryList = "webhook:delivery_list"
	WebhookDeliveryRetry = "webhook:delivery_retry"
	WebhookEndpointDelete = "webhook:endpoint_delete"
	WebhookEndpointList = "webhook:endpoint_list"
	WebhookEndpointSave = "webhook:endpoint_save"
	WebhookEndpointStatus = "webhook:endpoint_status"
)

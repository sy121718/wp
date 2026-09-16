package webhookdto

// webhook_endpoint_resp.go — webhook 端点与投递的响应。
//
// 两张响应结构都**不含密钥**：EndpointItem 只给 HasSecret 布尔，
// 投递日志只给负载预览。签名密钥是出站请求的可信凭证，读得回来就等于
// 「任何拿到后台读权限的人都能伪造我们的请求」。

import "time"

// EndpointItem 端点条目（无密钥字段）。
type EndpointItem struct {
	ID uint64 `json:"id"`
	// EventType 订阅的事件类型。
	EventType string `json:"eventType"`
	// TargetURL 目标地址。
	TargetURL string `json:"targetUrl"`
	// Description 备注。
	Description string `json:"description"`
	// Status 1 = 启用 / 0 = 停用。
	Status int `json:"status"`
	// HasSecret 是否已配置签名密钥（空密钥的端点发不出可验签的请求）。
	HasSecret bool `json:"hasSecret"`
	// CreateTime / UpdateTime 时间列（与全库口径一致，timestamptz）。
	CreateTime time.Time `json:"createTime"`
	UpdateTime time.Time `json:"updateTime"`
}

// DeliveryItem 一次投递的排障条目。
type DeliveryItem struct {
	ID         uint64 `json:"id"`
	EndpointID uint64 `json:"endpointId"`
	EventType  string `json:"eventType"`
	// PayloadPreview 负载预览（截断到 PayloadPreviewBytes）。
	// 不整段返回：单个事件上限 64KiB，一页 20 条就是兆级响应。
	PayloadPreview string `json:"payloadPreview"`
	// PayloadBytes 负载原始字节数（判断是否被截断）。
	PayloadBytes   int       `json:"payloadBytes"`
	Status         string    `json:"status"`
	Attempts       int       `json:"attempts"`
	ResponseStatus int       `json:"responseStatus"`
	LastError      string    `json:"lastError"`
	CreateTime     time.Time `json:"createTime"`
	UpdateTime     time.Time `json:"updateTime"`
}

// PayloadPreviewBytes 负载预览的截断长度。
const PayloadPreviewBytes = 200

// DeliveryListResp 投递日志列表。
type DeliveryListResp struct {
	Items []*DeliveryItem `json:"items"`
	Total int64           `json:"total"`
}

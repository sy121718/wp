package webhookdto

// webhook_endpoint_req.go — webhook 端点管理与投递查询的请求。

// SaveEndpointReq 新建 / 更新端点（ID 为 0 即新建）。
//
// Secret 是**明文**签名密钥，只在保存这一刻出现：service 立即加密落库，
// 此后没有任何接口能把它读回来（要换密钥就重填一次）。
type SaveEndpointReq struct {
	ID uint64 `form:"id" json:"id"`
	// EventType / TargetURL 不标 binding:"required"：更新是 PATCH 语义（空即不改），
	// 必填性由 service 按「新建 or 更新」分别判定 —— 标在这里会把「只改备注」也拦掉。
	EventType string `form:"eventType" json:"eventType"`
	TargetURL string `form:"targetUrl" json:"targetUrl"`
	// Secret 为空表示不改密钥。
	Secret      string `form:"secret" json:"secret"`
	Description string `form:"description" json:"description"`
	// Status 为空指针表示「不改」：更新时只改描述不该把端点意外停用。
	Status *int `form:"status" json:"status"`
}

// SetEndpointStatusReq 启停端点。
type SetEndpointStatusReq struct {
	ID     uint64 `form:"id" json:"id" binding:"required"`
	Status int    `form:"status" json:"status"`
}

// DeliveryListReq 投递日志筛选（全部字段可选，零值即不筛选）。
type DeliveryListReq struct {
	EndpointID uint64 `form:"endpointId" json:"endpointId"`
	EventType  string `form:"eventType" json:"eventType"`
	Status     string `form:"status" json:"status"`
	Page       int    `form:"page" json:"page"`
	PageSize   int    `form:"pageSize" json:"pageSize"`
}

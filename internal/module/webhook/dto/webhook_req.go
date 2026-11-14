package webhookdto

type SaveEndpointReq struct {
	ID uint64 `form:"id" json:"id"`

	EventType string `form:"eventType" json:"eventType"`
	TargetURL string `form:"targetUrl" json:"targetUrl"`

	Secret      string `form:"secret" json:"secret"`
	Description string `form:"description" json:"description"`

	Status *int `form:"status" json:"status"`
}

type SetEndpointStatusReq struct {
	ID     uint64 `form:"id" json:"id" binding:"required"`
	Status int    `form:"status" json:"status"`
}

type DeliveryListReq struct {
	EndpointID uint64 `form:"endpointId" json:"endpointId"`
	EventType  string `form:"eventType" json:"eventType"`
	Status     string `form:"status" json:"status"`
	Page       int    `form:"page" json:"page"`
	PageSize   int    `form:"pageSize" json:"pageSize"`
}

// EndpointIDQuery 是 webhook 删除 / 重试的只读查询串：id 只在 URL 上，body 不参与。
type EndpointIDQuery struct {
	ID string `form:"id"`
}

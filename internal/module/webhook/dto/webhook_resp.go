package webhookdto

import (
	"go_wp/pkg/utils"
)

type EndpointItem struct {
	ID uint64 `json:"id"`

	EventType string `json:"eventType"`

	TargetURL string `json:"targetUrl"`

	Description string `json:"description"`

	Status int `json:"status"`

	HasSecret bool `json:"hasSecret"`

	CreateTime utils.JSONTime `json:"createTime"`
	UpdateTime utils.JSONTime `json:"updateTime"`
}

type DeliveryItem struct {
	ID         uint64 `json:"id"`
	EndpointID uint64 `json:"endpointId"`
	EventType  string `json:"eventType"`

	PayloadPreview string `json:"payloadPreview"`

	PayloadBytes   int            `json:"payloadBytes"`
	Status         string         `json:"status"`
	Attempts       int            `json:"attempts"`
	ResponseStatus int            `json:"responseStatus"`
	LastError      string         `json:"lastError"`
	CreateTime     utils.JSONTime `json:"createTime"`
	UpdateTime     utils.JSONTime `json:"updateTime"`
}

const PayloadPreviewBytes = 200

type DeliveryListResp struct {
	Items []*DeliveryItem `json:"items"`
	Total int64           `json:"total"`
}

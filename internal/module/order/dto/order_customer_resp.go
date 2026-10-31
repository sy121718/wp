package orderdto

import (
	"go_wp/pkg/utils"
)

type CustomerOrderSummaryResp struct {
	UserID    uint64 `json:"userId"`
	ProjectID string `json:"projectId"`

	OrderCount int64 `json:"orderCount"`

	PaidOrderCount int64 `json:"paidOrderCount"`

	TotalAmount      int64  `json:"totalAmount"`
	TotalAmountLabel string `json:"totalAmountLabel"`

	LastOrderID       uint64          `json:"lastOrderId"`
	LastOrderNo       string          `json:"lastOrderNo"`
	LastOrderStatus   string          `json:"lastOrderStatus"`
	LastOrderTime     *utils.JSONTime `json:"lastOrderTime"`
	LastOrderTimeText string          `json:"lastOrderTimeText"`

	HasOrders bool `json:"hasOrders"`
}

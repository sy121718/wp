package purchasedto

import (
	"go_wp/pkg/utils"
)

type PurchaseLineResp struct {
	ID                  string  `json:"id"`
	ProductID           string  `json:"productId"`
	VariantID           string  `json:"variantId"`
	SKUCode             string  `json:"skuCode"`
	Quantity            int     `json:"quantity"`
	ReceivedQuantity    int     `json:"receivedQuantity"`
	OutstandingQuantity int     `json:"outstandingQuantity"`
	UnitPrice           float64 `json:"unitPrice"`
	Sort                int     `json:"sort"`
	Remark              string  `json:"remark"`
}

type PurchaseOrderResp struct {
	ID            string `json:"id"`
	ProjectID     string `json:"projectId"`
	Code          string `json:"code"`
	SourceID      string `json:"sourceId"`
	SourceName    string `json:"sourceName"`
	SourceType    string `json:"sourceType"`
	WarehouseID   string `json:"warehouseId"`
	WarehouseName string `json:"warehouseName"`

	Status     string          `json:"status"`
	OrderedAt  utils.JSONTime  `json:"orderedAt"`
	ExpectedAt *utils.JSONTime `json:"expectedAt"`
	Remark     string          `json:"remark"`
	OperatorID string          `json:"operatorId"`

	TotalQuantity       int                 `json:"totalQuantity"`
	ReceivedQuantity    int                 `json:"receivedQuantity"`
	OutstandingQuantity int                 `json:"outstandingQuantity"`
	ProgressPercent     int                 `json:"progressPercent"`
	Lines               []*PurchaseLineResp `json:"lines"`
	CreatedAt           utils.JSONTime      `json:"createdAt"`
	UpdatedAt           utils.JSONTime      `json:"updatedAt"`
}

type ReceiptItemResp struct {
	LineID      string  `json:"lineId"`
	ProductID   string  `json:"productId"`
	VariantID   string  `json:"variantId"`
	SKUCode     string  `json:"skuCode"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unitPrice"`
	CostUpdated bool    `json:"costUpdated"`
	CostError   string  `json:"costError"`
}

type ReceiptResp struct {
	ID              string         `json:"id"`
	Code            string         `json:"code"`
	Kind            string         `json:"kind"`
	OrderID         string         `json:"orderId"`
	OrderCode       string         `json:"orderCode"`
	SourceID        string         `json:"sourceId"`
	SourceName      string         `json:"sourceName"`
	WarehouseID     string         `json:"warehouseId"`
	WarehouseName   string         `json:"warehouseName"`
	Status          string         `json:"status"`
	MovementBatchID string         `json:"movementBatchId"`
	Remark          string         `json:"remark"`
	OperatorID      string         `json:"operatorId"`
	ReceivedAt      utils.JSONTime `json:"receivedAt"`

	CostUpdated bool               `json:"costUpdated"`
	Idempotent  bool               `json:"idempotent"`
	Items       []*ReceiptItemResp `json:"items"`
}

type PurchaseHistoryResp struct {
	ReceiptID       string         `json:"receiptId"`
	ReceiptCode     string         `json:"receiptCode"`
	Kind            string         `json:"kind"`
	OrderID         string         `json:"orderId"`
	OrderCode       string         `json:"orderCode"`
	SourceID        string         `json:"sourceId"`
	SourceName      string         `json:"sourceName"`
	SourceType      string         `json:"sourceType"`
	WarehouseID     string         `json:"warehouseId"`
	WarehouseName   string         `json:"warehouseName"`
	VariantID       string         `json:"variantId"`
	SKUCode         string         `json:"skuCode"`
	Quantity        int            `json:"quantity"`
	UnitPrice       float64        `json:"unitPrice"`
	CostUpdated     bool           `json:"costUpdated"`
	MovementBatchID string         `json:"movementBatchId"`
	Remark          string         `json:"remark"`
	OperatorID      string         `json:"operatorId"`
	ReceivedAt      utils.JSONTime `json:"receivedAt"`
}

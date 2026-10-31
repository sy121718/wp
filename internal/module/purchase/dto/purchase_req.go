package purchasedto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

type PurchaseLineReq struct {
	VariantID string  `json:"variantId" form:"variantId" binding:"required"`
	ProductID string  `json:"productId" form:"productId"`
	SKUCode   string  `json:"skuCode" form:"skuCode"`
	Quantity  int     `json:"quantity" form:"quantity"`
	UnitPrice float64 `json:"unitPrice" form:"unitPrice"`
	Sort      int     `json:"sort" form:"sort"`
	Remark    string  `json:"remark" form:"remark"`
}

type CreatePurchaseOrderReq struct {
	ProjectID   string            `json:"projectId" form:"projectId"`
	Code        string            `json:"code" form:"code"`
	SourceID    string            `json:"sourceId" form:"sourceId"`
	WarehouseID string            `json:"warehouseId" form:"warehouseId"`
	ExpectedAt  *utils.JSONTime   `json:"expectedAt" form:"expectedAt"`
	Remark      string            `json:"remark" form:"remark"`
	OperatorID  string            `json:"operatorId" form:"operatorId"`
	AsDraft     bool              `json:"asDraft" form:"asDraft"`
	Metadata    json.RawMessage   `json:"metadata" form:"metadata"`
	Lines       []PurchaseLineReq `json:"lines" form:"lines"`
}

type UpdatePurchaseOrderReq struct {
	ID              string            `json:"id" form:"id" binding:"required"`
	ProjectID       string            `json:"projectId" form:"projectId"`
	SourceID        *string           `json:"sourceId" form:"sourceId"`
	WarehouseID     *string           `json:"warehouseId" form:"warehouseId"`
	ExpectedAt      *utils.JSONTime   `json:"expectedAt" form:"expectedAt"`
	ClearExpectedAt bool              `json:"clearExpectedAt" form:"clearExpectedAt"`
	Remark          *string           `json:"remark" form:"remark"`
	OperatorID      *string           `json:"operatorId" form:"operatorId"`
	Metadata        json.RawMessage   `json:"metadata" form:"metadata"`
	ReplaceLines    bool              `json:"replaceLines" form:"replaceLines"`
	Lines           []PurchaseLineReq `json:"lines" form:"lines"`
}

type GetPurchaseOrderReq struct {
	ID        string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
}

type ListPurchaseOrderReq struct {
	ProjectID string `form:"projectId"`
	Status    string `form:"status"`
	SourceID  string `form:"sourceId"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}

type ReceiptLineReq struct {
	LineID    string   `json:"lineId" form:"lineId" binding:"required"`
	Quantity  int      `json:"quantity" form:"quantity"`
	UnitPrice *float64 `json:"unitPrice" form:"unitPrice"`
}

type RegisterReceiptReq struct {
	ProjectID   string           `json:"projectId" form:"projectId"`
	OrderID     string           `json:"orderId" form:"orderId" binding:"required"`
	WarehouseID string           `json:"warehouseId" form:"warehouseId"`
	RequestID   string           `json:"requestId" form:"requestId"`
	Remark      string           `json:"remark" form:"remark"`
	OperatorID  string           `json:"operatorId" form:"operatorId"`
	ReceivedAt  *utils.JSONTime  `json:"receivedAt" form:"receivedAt"`
	Lines       []ReceiptLineReq `json:"lines" form:"lines" binding:"required"`
}

type ProductionInboundReq struct {
	ProjectID   string `json:"projectId" form:"projectId"`
	SourceID    string `json:"sourceId" form:"sourceId" binding:"required"`
	WarehouseID string `json:"warehouseId" form:"warehouseId"`
	ProductID   string `json:"productId" form:"productId"`
	VariantID   string `json:"variantId" form:"variantId" binding:"required"`
	SKUCode     string `json:"skuCode" form:"skuCode"`
	Quantity    int    `json:"quantity" form:"quantity"`

	UnitCost   *float64        `json:"unitCost" form:"unitCost"`
	RequestID  string          `json:"requestId" form:"requestId"`
	Remark     string          `json:"remark" form:"remark"`
	OperatorID string          `json:"operatorId" form:"operatorId"`
	ReceivedAt *utils.JSONTime `json:"receivedAt" form:"receivedAt"`
}

type ListPurchaseHistoryReq struct {
	ProjectID string `form:"projectId"`
	SKUCode   string `form:"skuCode"`
	VariantID string `form:"variantId"`
	SourceID  string `form:"sourceId"`
	OrderID   string `form:"orderId"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}

type PurchaseStatusReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	ID        string `json:"id" form:"id" binding:"required"`
	Remark    string `json:"remark" form:"remark"`
}

type PurchaseListQueryReq struct {
	ProjectID string `form:"project"`
	Status    string `form:"status"`
	SourceID  string `form:"sourceId"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
}

// inventory_purchase_resp.go — 采购单与入库出参（issue #18）。
package inventorydto

import "time"

// PurchaseLineResp 一条采购行（含「已入库 / 未入库」两个推导量）。
//
// OutstandingQuantity = Quantity - ReceivedQuantity，是登记入库时「还能收多少」的
// 直接依据；状态推导读的也是这两个数（不是订单头上的状态列）。
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

// PurchaseOrderResp 采购单（含全部采购行与汇总数量）。
type PurchaseOrderResp struct {
	ID            string `json:"id"`
	ProjectID     string `json:"projectId"`
	Code          string `json:"code"`
	SourceID      string `json:"sourceId"`
	SourceName    string `json:"sourceName"`
	SourceType    string `json:"sourceType"`
	WarehouseID   string `json:"warehouseId"`
	WarehouseName string `json:"warehouseName"`
	// Status 是推导值：pending / partial / received（见 enums 的推导口径）。
	Status           string              `json:"status"`
	OrderedAt        time.Time           `json:"orderedAt"`
	ExpectedAt       *time.Time          `json:"expectedAt"`
	Remark           string              `json:"remark"`
	OperatorID       string              `json:"operatorId"`
	TotalQuantity    int                 `json:"totalQuantity"`
	ReceivedQuantity int                 `json:"receivedQuantity"`
	Lines            []*PurchaseLineResp `json:"lines"`
	CreatedAt        time.Time           `json:"createdAt"`
	UpdatedAt        time.Time           `json:"updatedAt"`
}

// ReceiptItemResp 一条入库行（数量 + 单价快照 + 成本价回写结果）。
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

// ReceiptResp 入库单（采购收货 / 生产入库共用）。
//
// Idempotent 为真表示本次提交命中了幂等键（返回的是既有入库单，没有第二次动库存）。
type ReceiptResp struct {
	ID              string    `json:"id"`
	Code            string    `json:"code"`
	Kind            string    `json:"kind"`
	OrderID         string    `json:"orderId"`
	OrderCode       string    `json:"orderCode"`
	SourceID        string    `json:"sourceId"`
	SourceName      string    `json:"sourceName"`
	WarehouseID     string    `json:"warehouseId"`
	WarehouseName   string    `json:"warehouseName"`
	Status          string    `json:"status"`
	MovementBatchID string    `json:"movementBatchId"`
	Remark          string    `json:"remark"`
	OperatorID      string    `json:"operatorId"`
	ReceivedAt      time.Time `json:"receivedAt"`
	// CostUpdated 为真表示本单全部行的 SKU 成本价都已按单价写回。
	CostUpdated bool               `json:"costUpdated"`
	Idempotent  bool               `json:"idempotent"`
	Items       []*ReceiptItemResp `json:"items"`
}

// PurchaseHistoryResp 一条进货历史（某 SKU 的历次入库）。
type PurchaseHistoryResp struct {
	ReceiptID       string    `json:"receiptId"`
	ReceiptCode     string    `json:"receiptCode"`
	Kind            string    `json:"kind"`
	OrderID         string    `json:"orderId"`
	OrderCode       string    `json:"orderCode"`
	SourceID        string    `json:"sourceId"`
	SourceName      string    `json:"sourceName"`
	SourceType      string    `json:"sourceType"`
	WarehouseID     string    `json:"warehouseId"`
	WarehouseName   string    `json:"warehouseName"`
	VariantID       string    `json:"variantId"`
	SKUCode         string    `json:"skuCode"`
	Quantity        int       `json:"quantity"`
	UnitPrice       float64   `json:"unitPrice"`
	CostUpdated     bool      `json:"costUpdated"`
	MovementBatchID string    `json:"movementBatchId"`
	Remark          string    `json:"remark"`
	OperatorID      string    `json:"operatorId"`
	ReceivedAt      time.Time `json:"receivedAt"`
}

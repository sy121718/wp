package orderdto

import "go_wp/pkg/utils"

type ShipmentResp struct {
	ID         string `json:"id"`
	ShipmentNo string `json:"shipmentNo"`
	OrderID    uint64 `json:"orderId"`
	CarrierID  string `json:"carrierId"`

	CarrierName  string `json:"carrierName"`
	TrackingNo   string `json:"trackingNo"`
	ShippedAt    string `json:"shippedAt"`
	OperatorID   int64  `json:"operatorId"`
	OperatorName string `json:"operatorName"`
	Remark       string `json:"remark"`
	RequestID    string `json:"requestId"`

	TrackingURL string `json:"trackingUrl"`

	Items []*ShipmentItemResp `json:"items,omitempty"`
}

type ShipmentItemResp struct {
	OrderItemID  uint64 `json:"orderItemId"`
	ProductName  string `json:"productName"`
	VariantLabel string `json:"variantLabel"`
	SKU          string `json:"sku"`
	WarehouseID  string `json:"warehouseId"`
	Quantity     int    `json:"quantity"`
}

type CarrierResp struct {
	ID                  string `json:"id"`
	Code                string `json:"code"`
	Name                string `json:"name"`
	TrackingURLTemplate string `json:"trackingUrlTemplate"`
	Status              string `json:"status"`
	Sort                int    `json:"sort"`
}

type PendingShipRow struct {
	OrderID      uint64 `json:"orderId"`
	OrderNo      string `json:"orderNo"`
	CustomerName string `json:"customerName"`

	CustomerEmail string `json:"customerEmail"`

	Currency string `json:"currency"`
	Total    int64  `json:"total"`

	LineCount         int `json:"lineCount"`
	ItemQuantity      int `json:"itemQuantity"`
	ShippedQuantity   int `json:"shippedQuantity"`
	RemainingQuantity int `json:"remainingQuantity"`

	OrderedAt utils.JSONTime `json:"orderedAt"`

	LatestShipmentNo string `json:"latestShipmentNo"`

	CreatedVia string `json:"createdVia"`

	Shippable bool `json:"shippable"`

	ShippableReason string `json:"shippableReason"`
}

type PendingShipPageResp struct {
	List  []*PendingShipRow `json:"list"`
	Total int64             `json:"total"`
}

type ShipFormLine struct {
	OrderItemID  uint64 `json:"orderItemId"`
	ProductName  string `json:"productName"`
	VariantLabel string `json:"variantLabel"`
	SKU          string `json:"sku"`
	Quantity     int    `json:"quantity"`
	Shipped      int    `json:"shipped"`

	Remaining int `json:"remaining"`

	WarehouseID   string `json:"warehouseId"`
	WarehouseName string `json:"warehouseName"`
}

type ShipForm struct {
	OrderID uint64 `json:"orderId"`

	RequestID    string          `json:"requestId"`
	OrderNo      string          `json:"orderNo"`
	CustomerName string          `json:"customerName"`
	Total        int64           `json:"total"`
	Lines        []*ShipFormLine `json:"lines"`
	Carriers     []*CarrierResp  `json:"carriers"`
}

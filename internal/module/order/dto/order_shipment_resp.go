package orderdto

// order_shipment_resp.go — 发货 / 承运商 / 待发货的响应（迁移 624 / 625，S2）。

// ShipmentResp 一张发货单。
type ShipmentResp struct {
	ID         string `json:"id"`
	ShipmentNo string `json:"shipmentNo"`
	OrderID    uint64 `json:"orderId"`
	CarrierID  string `json:"carrierId"`
	// CarrierName 是**名称快照**（承运商改名 / 停用 / 删除之后仍显示当时的名字）。
	CarrierName  string `json:"carrierName"`
	TrackingNo   string `json:"trackingNo"`
	ShippedAt    string `json:"shippedAt"`
	OperatorID   int64  `json:"operatorId"`
	OperatorName string `json:"operatorName"`
	Remark       string `json:"remark"`
	RequestID    string `json:"requestId"`
	// TrackingURL 由承运商模板 + 运单号现算（模板为空时也是空串）。
	TrackingURL string `json:"trackingUrl"`
	// Items 本次发货的明细（详情抽屉用；列表路径不带）。
	Items []*ShipmentItemResp `json:"items,omitempty"`
}

// ShipmentItemResp 一行发货明细（含订单行的商品快照，展示不必再查订单）。
type ShipmentItemResp struct {
	OrderItemID  uint64 `json:"orderItemId"`
	ProductName  string `json:"productName"`
	VariantLabel string `json:"variantLabel"`
	SKU          string `json:"sku"`
	WarehouseID  string `json:"warehouseId"`
	Quantity     int    `json:"quantity"`
}

// CarrierResp 承运商。
type CarrierResp struct {
	ID                  string `json:"id"`
	Code                string `json:"code"`
	Name                string `json:"name"`
	TrackingURLTemplate string `json:"trackingUrlTemplate"`
	Status              string `json:"status"`
	Sort                int    `json:"sort"`
}

// PendingShipRow 待发货列表的一行。
//
// 六个数字都由 service 算好（ItemQuantity / ShippedQuantity / RemainingQuantity）：
// 模板**不做减法**（template_logic_test.go）—— 那条判据针对的正是「剩余 = 件数 − 已发」
// 这种看起来无害的算式，它在页面上一出现就会与 service 的口径分叉。
type PendingShipRow struct {
	OrderID           uint64 `json:"orderId"`
	OrderNo           string `json:"orderNo"`
	CustomerName      string `json:"customerName"`
	Total             int64  `json:"total"`
	ItemQuantity      int    `json:"itemQuantity"`
	ShippedQuantity   int    `json:"shippedQuantity"`
	RemainingQuantity int    `json:"remainingQuantity"`
	OrderedAt         string `json:"orderedAt"`
	// LatestShipmentNo 最近一次发货的单号（还没发过时为空串，页面显示「—」）。
	LatestShipmentNo string `json:"latestShipmentNo"`
}

// ShipFormLine 发货表单的一行（service 给全「订单数量 / 已发 / 剩余」，表单只填「本次」）。
type ShipFormLine struct {
	OrderItemID  uint64 `json:"orderItemId"`
	ProductName  string `json:"productName"`
	VariantLabel string `json:"variantLabel"`
	SKU          string `json:"sku"`
	Quantity     int    `json:"quantity"`
	Shipped      int    `json:"shipped"`
	// Remaining 这一行还能发多少（= Quantity − Shipped，算在这里而不是模板）。
	Remaining int `json:"remaining"`
	// WarehouseID 默认发货仓（下单时固化；存量行为空串，由运营在表单里选）。
	WarehouseID   string `json:"warehouseId"`
	WarehouseName string `json:"warehouseName"`
}

// ShipForm 发货表单的数据（订单头只读信息 + 明细 + 承运商下拉）。
type ShipForm struct {
	OrderID uint64 `json:"orderId"`
	// RequestID 本次渲染的幂等键：双击提交 / 网络重试只会产生一张发货单。
	// 由 service 生成（与「这一次操作」绑定，而不是与订单绑定）。
	RequestID    string          `json:"requestId"`
	OrderNo      string          `json:"orderNo"`
	CustomerName string          `json:"customerName"`
	Total        int64           `json:"total"`
	Lines        []*ShipFormLine `json:"lines"`
	Carriers     []*CarrierResp  `json:"carriers"`
}

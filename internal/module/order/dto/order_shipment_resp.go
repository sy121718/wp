package orderdto

import "go_wp/pkg/utils"

// order_shipment_resp.go — 发货 / 承运商 / 待发货的响应（迁移 627 / 625，S2）。

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
	OrderID      uint64 `json:"orderId"`
	OrderNo      string `json:"orderNo"`
	CustomerName string `json:"customerName"`
	// CustomerEmail / CustomerPhone 是拣货时辨认收件人的补充事实（列表不展示，发货抽屉用）。
	CustomerEmail string `json:"customerEmail"`
	// Currency 订单币种（金额展示的前缀由模板的 currencyPrefix 决定）。
	Currency string `json:"currency"`
	Total    int64  `json:"total"`
	// LineCount 订单行数（拣货要备几种货）；ItemQuantity 是件数合计（同一种货要备几件）。
	LineCount         int `json:"lineCount"`
	ItemQuantity      int `json:"itemQuantity"`
	ShippedQuantity   int `json:"shippedQuantity"`
	RemainingQuantity int `json:"remainingQuantity"`
	// OrderedAt 下单时刻（时间值而不是预格式化串：日期格式属于展示，走模板的 dateTime）。
	OrderedAt utils.JSONTime `json:"orderedAt"`
	// LatestShipmentNo 最近一次发货的单号（还没发过时为空串，页面显示「—」）。
	LatestShipmentNo string `json:"latestShipmentNo"`
	// CreatedVia 订单来源事实（checkout = 访客结算 / admin = 后台自建 / api = 接口）。
	//
	// 原值直接给模板：词条是 admin.orders.created_via.*（订单列表页已在用），
	// service 不在这里取词 —— 取词是出口的事。
	CreatedVia string `json:"createdVia"`
	// Shippable 这一单现在能不能发（B6-a 的守卫，对齐 dy_erp 的「行没匹配上就禁用」）。
	//
	// 由 service 按发货用例的真实判据算出（变体缺失 / 无仓 / 跟踪行库存不足 → false），
	// 模板只读它决定按钮 disabled —— 真正的胜负仍由发货事务的行锁判定。
	Shippable bool `json:"shippable"`
	// ShippableReason 不可发时的**事实码**（order.err.* 之外的界面词条 key 由模板决定）：
	// variant_missing / warehouse_missing / stock_insufficient / request_invalid。
	//
	// 给码不给句：模板拿它挑词条（admin.order.pendingShip.blocked.<code>），
	// 而 service 不写界面 key 字面量（AGENTS.md 的取词约定）。
	ShippableReason string `json:"shippableReason"`
}

// PendingShipPageResp 待发货页的一页（列表 + 分页总数）。
//
// 为什么单独一个响应而不是裸切片：分页条要的是「筛完之后一共多少单」，
// 那是与行集合**不同**的一件事实；塞进行里会变成每行都带同一个总数。
type PendingShipPageResp struct {
	List  []*PendingShipRow `json:"list"`
	Total int64             `json:"total"`
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

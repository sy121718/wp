package orderdto

// order_shipment_req.go — 发货 / 承运商 / 发货单查询的入参（迁移 624 / 625，S2）。

// ShipLineReq 一行发货明细。
//
// OrderItemID 是「发哪一行」，WarehouseID 是「从哪个仓发」—— 两者合起来才唯一确定
// 一次发货事实（同一订单行的不同仓可以分两次发）。
type ShipLineReq struct {
	OrderItemID uint64 `form:"orderItemId" json:"orderItemId" binding:"required"`
	// WarehouseID 空 = 用 order_items.warehouse_id（下单时已固化）；
	// 存量行（NULL）时空值会触发「解析默认仓并回写」。
	WarehouseID string `form:"warehouseId" json:"warehouseId"`
	Quantity    int    `form:"quantity" json:"quantity"`
}

// ShipOrderReq 发货请求。
//
// RequestID 是幂等键：同一 (工程, request_id) 只会产生一张发货单，重复提交直接返回那张单
// （不改库存、不改状态）。前端不传时留空 —— 空串是「这次没有幂等键」这个合法状态。
type ShipOrderReq struct {
	// OrderID 订单 id（路径/表单给）；ProjectID 是工程作用域（为空时逐工程探测归属）。
	OrderID   uint64 `form:"orderId" json:"orderId" binding:"required"`
	ProjectID string `form:"projectId" json:"projectId"`
	RequestID string `form:"requestId" json:"requestId"`
	// CarrierID 承运商（可空：自营配送 / 客户自提没有承运商）。
	CarrierID   string `form:"carrierId" json:"carrierId"`
	CarrierName string `form:"carrierName" json:"carrierName"`
	TrackingNo  string `form:"trackingNo" json:"trackingNo"`
	Remark      string `form:"remark" json:"remark"`
	// OperatorID / OperatorName 是**审计字段**：服务端从会话无条件覆盖（见 handler），
	// 客户端传什么都不看。仍带 form tag 只为满足「写 DTO 字段都带 form tag」的形状判据 ——
	// 它们的值永远不会来自请求体。
	OperatorID   int64         `form:"operatorId" json:"operatorId"`
	OperatorName string        `form:"operatorName" json:"operatorName"`
	Lines        []ShipLineReq `form:"lines" json:"lines" binding:"required"`
}

// ListShipmentReq 某订单的发货记录。
type ListShipmentReq struct {
	OrderID   uint64 `form:"orderId" json:"orderId" binding:"required"`
	ProjectID string `form:"projectId" json:"projectId"`
}

// CarrierCreateReq 新建承运商。
type CarrierCreateReq struct {
	ProjectID           string `form:"projectId" json:"projectId"`
	Code                string `form:"code" json:"code" binding:"required"`
	Name                string `form:"name" json:"name" binding:"required"`
	TrackingURLTemplate string `form:"trackingUrlTemplate" json:"trackingUrlTemplate"`
	Sort                int    `form:"sort" json:"sort"`
}

// CarrierUpdateReq 修改承运商（逐字段可选；nil = 本次不改）。
type CarrierUpdateReq struct {
	ID        string  `form:"id" json:"id" binding:"required"`
	ProjectID string  `form:"projectId" json:"projectId"`
	Code      *string `form:"code" json:"code"`
	Name      *string `form:"name" json:"name"`
	// TrackingURLTemplate nil = 不改；空串 = 清空（没有公开跟踪页）。
	TrackingURLTemplate *string `form:"trackingUrlTemplate" json:"trackingUrlTemplate"`
	Status              *string `form:"status" json:"status"`
	Sort                *int    `form:"sort" json:"sort"`
}

// ListCarrierReq 承运商列表（后台配置页 / 发货表单下拉）。
type ListCarrierReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	// IncludeDisabled 后台配置页要看到停用的（否则「停用后不见了」会让人以为被删了）；
	// 发货表单不传，只看启用中的。
	IncludeDisabled bool `form:"includeDisabled" json:"includeDisabled"`
}

// PendingShipReq 待发货列表的查询串（工程维度）。
type PendingShipReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
}

// ShipmentPageQueryReq 发货三屏的查询串（工程上下文）。
//
// 键名是已发布 URL 的一部分（书签 / 外部链接都按它拼），所以 form tag 不能随手改。
type ShipmentPageQueryReq struct {
	ProjectID string `form:"project" json:"project"`
}

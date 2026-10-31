package orderdto

type ShipLineReq struct {
	OrderItemID uint64 `form:"orderItemId" json:"orderItemId" binding:"required"`

	WarehouseID string `form:"warehouseId" json:"warehouseId"`
	Quantity    int    `form:"quantity" json:"quantity"`
}

type ShipOrderReq struct {
	OrderID   uint64 `form:"orderId" json:"orderId" binding:"required"`
	ProjectID string `form:"projectId" json:"projectId"`
	RequestID string `form:"requestId" json:"requestId"`

	CarrierID   string `form:"carrierId" json:"carrierId"`
	CarrierName string `form:"carrierName" json:"carrierName"`
	TrackingNo  string `form:"trackingNo" json:"trackingNo"`
	Remark      string `form:"remark" json:"remark"`

	OperatorID   int64         `form:"operatorId" json:"operatorId"`
	OperatorName string        `form:"operatorName" json:"operatorName"`
	Lines        []ShipLineReq `form:"lines" json:"lines" binding:"required"`
}

type ListShipmentReq struct {
	OrderID   uint64 `form:"orderId" json:"orderId" binding:"required"`
	ProjectID string `form:"projectId" json:"projectId"`
}

type CarrierCreateReq struct {
	ProjectID           string `form:"projectId" json:"projectId"`
	Code                string `form:"code" json:"code" binding:"required"`
	Name                string `form:"name" json:"name" binding:"required"`
	TrackingURLTemplate string `form:"trackingUrlTemplate" json:"trackingUrlTemplate"`
	Sort                int    `form:"sort" json:"sort"`
}

type CarrierUpdateReq struct {
	ID        string  `form:"id" json:"id" binding:"required"`
	ProjectID string  `form:"projectId" json:"projectId"`
	Code      *string `form:"code" json:"code"`
	Name      *string `form:"name" json:"name"`

	TrackingURLTemplate *string `form:"trackingUrlTemplate" json:"trackingUrlTemplate"`
	Status              *string `form:"status" json:"status"`
	Sort                *int    `form:"sort" json:"sort"`
}

type ListCarrierReq struct {
	ProjectID string `form:"projectId" json:"projectId"`

	IncludeDisabled bool `form:"includeDisabled" json:"includeDisabled"`
}

type PendingShipReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	Keyword   string `form:"keyword" json:"keyword"`
	Offset    int    `form:"offset" json:"offset"`
	Limit     int    `form:"limit" json:"limit"`
}

type ShipmentPageQueryReq struct {
	ProjectID string `form:"project" json:"project"`
	Keyword   string `form:"keyword" json:"keyword"`
	Page      int    `form:"page" json:"page"`
	Limit     int    `form:"limit" json:"limit"`
}

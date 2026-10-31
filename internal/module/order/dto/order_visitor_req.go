package orderdto

type VisitorOrderListReq struct {
	ProjectID string `json:"projectId" form:"projectId" binding:"required,uuid"`
	Status    string `json:"status" form:"status"`
	Offset    int    `json:"offset" form:"offset" binding:"min=0"`
	Limit     int    `json:"limit" form:"limit" binding:"min=0,max=50"`
	UserID    uint64 `json:"-" form:"-"`
}

type VisitorOrderDetailReq struct {
	OrderID   uint64 `json:"orderId" form:"orderId" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId" binding:"required,uuid"`
	UserID    uint64 `json:"-" form:"-"`
}

package orderdto

// order_return_req.go — 退货入库的请求（BIZ-1）。
//
// 退货与取消是两件事：取消发生在货还没出去的时候，退货发生在货已经出去之后 ——
// 后者有**实物验收**环节，所以退款与入库可以不同步（客户寄回、仓库点货、财务退款
// 是三个人在不同时间做的事）。请求形状必须能表达「已批准但还没收到货」这个中间态。

// ReturnItemReq 退货明细：按**订单项**选数量（部分退货是常态）。
type ReturnItemReq struct {
	OrderItemID uint64 `json:"orderItemId" form:"orderItemId"`
	Quantity    int    `json:"quantity" form:"quantity"`
}

// ReturnRequestReq 客户提交退货申请。
type ReturnRequestReq struct {
	ProjectID string          `json:"projectId" form:"projectId"`
	OrderID   uint64          `json:"orderId" form:"orderId"`
	Items     []ReturnItemReq `json:"items"`
	Reason    string          `json:"reason" form:"reason"`
	// RequestID 幂等键：页面渲染时生成一次，重复提交只落一张申请单。
	RequestID string `json:"requestId" form:"requestId"`

	// 以下由调用方（片段层 / inbound）覆盖写入，客户端不可伪造：
	// 归属校验靠它，能被表单指定就等于任何人都能替别人申请退货。
	UserID    uint64 `json:"-" form:"-"`
	IPAddress string `json:"-" form:"-"`
}

// ReturnReviewReq 后台审核（同意 / 拒绝）。
type ReturnReviewReq struct {
	ReturnID uint64 `json:"returnId" form:"returnId"`
	Remark   string `json:"remark" form:"remark"`
	// AutoReceive 同意后**立即**完成「入库 + 退款」。
	//
	// 现实里货往往早就到了（客户先联系客服、客服再走系统），所以一步到底才是常态；
	// 不勾选时停在 approved，等仓库点完货再点「确认收货并退货」。
	AutoReceive bool `json:"autoReceive" form:"autoReceive"`
	// WarehouseID 入库仓库（空 = 该工程默认仓）。
	WarehouseID   string `json:"warehouseId" form:"warehouseId"`
	TransactionID string `json:"transactionId" form:"transactionId"`

	OperatorType string `json:"-" form:"-"`
	OperatorID   uint64 `json:"-" form:"-"`
	OperatorName string `json:"-" form:"-"`
}

// ReturnReceiveReq 确认收货：入库 + 退款（两步都可重试，各自幂等）。
type ReturnReceiveReq struct {
	ReturnID      uint64 `json:"returnId" form:"returnId"`
	WarehouseID   string `json:"warehouseId" form:"warehouseId"`
	TransactionID string `json:"transactionId" form:"transactionId"`
	Remark        string `json:"remark" form:"remark"`

	OperatorType string `json:"-" form:"-"`
	OperatorID   uint64 `json:"-" form:"-"`
	OperatorName string `json:"-" form:"-"`
}

// ReturnCancelReq 客户撤销申请（只有还没审核的能撤）。
type ReturnCancelReq struct {
	ReturnID uint64 `json:"returnId" form:"returnId"`
	Reason   string `json:"reason" form:"reason"`
	// UserID 由片段层写入：撤销的是**自己的**申请。
	UserID uint64 `json:"-" form:"-"`
}

// ReturnListReq 后台退货申请列表。
type ReturnListReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	Status    string `form:"status" json:"status"`
	Keyword   string `form:"keyword" json:"keyword"`
	OrderID   uint64 `form:"orderId" json:"orderId"`
	Offset    int    `form:"offset" json:"offset"`
	Limit     int    `form:"limit" json:"limit"`
}

// VisitorReturnListReq 访客查自己的退货申请。
type VisitorReturnListReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	OrderID   uint64 `form:"orderId" json:"orderId"`
	Offset    int    `form:"offset" json:"offset"`
	Limit     int    `form:"limit" json:"limit"`

	// UserID 由片段层写入；为 0 一律拒绝（「不传即全部」是越权）。
	UserID uint64 `form:"-" json:"-"`
}

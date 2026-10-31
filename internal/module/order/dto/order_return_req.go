package orderdto

import (
	"go_wp/pkg/jsonx"
	"net/url"
	"strconv"
)

type ReturnItemReq struct {
	OrderItemID uint64 `json:"orderItemId" form:"orderItemId"`
	Quantity    int    `json:"quantity" form:"quantity"`
}

type ReturnRequestReq struct {
	ProjectID string          `json:"projectId" form:"projectId" binding:"required,uuid"`
	OrderID   uint64          `json:"orderId" form:"orderId" binding:"required"`
	Items     []ReturnItemReq `json:"items" form:"-"`
	Reason    string          `json:"reason" form:"reason"`

	RequestID string `json:"requestId" form:"requestId" binding:"required,uuid"`

	UserID    uint64 `json:"-" form:"-"`
	IPAddress string `json:"-" form:"-"`
}

type ReturnReviewReq struct {
	ReturnID uint64 `json:"returnId" form:"returnId"`
	Remark   string `json:"remark" form:"remark"`

	AutoReceive bool `json:"autoReceive" form:"autoReceive"`

	WarehouseID   string `json:"warehouseId" form:"warehouseId"`
	TransactionID string `json:"transactionId" form:"transactionId"`

	OperatorType string `json:"-" form:"-"`
	OperatorID   uint64 `json:"-" form:"-"`
	OperatorName string `json:"-" form:"-"`
}

type ReturnReceiveReq struct {
	ReturnID      uint64 `json:"returnId" form:"returnId"`
	WarehouseID   string `json:"warehouseId" form:"warehouseId"`
	TransactionID string `json:"transactionId" form:"transactionId"`
	Remark        string `json:"remark" form:"remark"`

	OperatorType string `json:"-" form:"-"`
	OperatorID   uint64 `json:"-" form:"-"`
	OperatorName string `json:"-" form:"-"`
}

type ReturnCancelReq struct {
	ReturnID uint64 `json:"returnId" form:"returnId"`
	Reason   string `json:"reason" form:"reason"`

	UserID uint64 `json:"-" form:"-"`
}

type ReturnListReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	Status    string `form:"status" json:"status"`
	Keyword   string `form:"keyword" json:"keyword"`
	OrderID   uint64 `form:"orderId" json:"orderId"`
	Offset    int    `form:"offset" json:"offset"`
	Limit     int    `form:"limit" json:"limit"`
}

type VisitorReturnListReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required,uuid"`
	OrderID   uint64 `form:"orderId" json:"orderId" binding:"required"`
	Offset    int    `form:"offset" json:"offset" binding:"min=0"`
	Limit     int    `form:"limit" json:"limit" binding:"min=0,max=50"`

	UserID uint64 `form:"-" json:"-"`
}

func (r *ReturnRequestReq) BindForm(v url.Values) error {
	if raw := v.Get("items"); raw != "" {
		return jsonx.Unmarshal([]byte(raw), &r.Items)
	}
	ids, quantities := v["orderItemId"], v["quantity"]
	if len(ids) != len(quantities) {
		return strconv.ErrSyntax
	}
	r.Items = make([]ReturnItemReq, 0, len(ids))
	for i, raw := range ids {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return err
		}
		qty, err := strconv.Atoi(quantities[i])
		if err != nil {
			return err
		}
		r.Items = append(r.Items, ReturnItemReq{OrderItemID: id, Quantity: qty})
	}
	return nil
}

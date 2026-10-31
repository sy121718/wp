package inventorydto

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type TransferLineReq struct {
	StockID  string `form:"stockId" json:"stockId" binding:"required"`
	Quantity int    `form:"quantity" json:"quantity"`
}

type CreateTransferReq struct {
	ProjectID       string            `form:"projectId" json:"projectId"`
	FromWarehouseID string            `form:"fromWarehouseId" json:"fromWarehouseId" binding:"required"`
	ToWarehouseID   string            `form:"toWarehouseId" json:"toWarehouseId" binding:"required"`
	Remark          string            `form:"remark" json:"remark"`
	OperatorID      int64             `form:"operatorId" json:"operatorId"`
	RequestID       string            `form:"requestId" json:"requestId"`
	Lines           []TransferLineReq `form:"lines" json:"lines" binding:"required"`

	AttachmentIDs []int64 `form:"attachmentIds" json:"attachmentIds"`
}

func (r *CreateTransferReq) BindForm(vals url.Values) error {
	lines := make([]TransferLineReq, 0, 4)
	for idx := 0; idx < maxTransferFormLines; idx++ {
		base := fmt.Sprintf("lines[%d].", idx)
		stockID := strings.TrimSpace(vals.Get(base + "stockId"))
		if stockID == "" {

			continue
		}
		qty := 0
		if raw := strings.TrimSpace(vals.Get(base + "quantity")); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {

				return fmt.Errorf("quantity of %s is not an integer", base)
			}
			qty = n
		}
		lines = append(lines, TransferLineReq{StockID: stockID, Quantity: qty})
	}
	if len(lines) > 0 {
		r.Lines = lines
	}
	return nil
}

const maxTransferFormLines = 200

type TransferShipReq struct {
	ProjectID     string  `form:"projectId" json:"projectId"`
	ID            string  `form:"id" json:"id" binding:"required"`
	Remark        string  `form:"remark" json:"remark"`
	OperatorID    int64   `form:"operatorId" json:"operatorId"`
	AttachmentIDs []int64 `form:"attachmentIds" json:"attachmentIds"`
}

type TransferReceiveReq struct {
	ProjectID     string  `form:"projectId" json:"projectId"`
	ID            string  `form:"id" json:"id" binding:"required"`
	Remark        string  `form:"remark" json:"remark"`
	OperatorID    int64   `form:"operatorId" json:"operatorId"`
	AttachmentIDs []int64 `form:"attachmentIds" json:"attachmentIds"`
}

type TransferApproveReq struct {
	ProjectID     string `form:"projectId" json:"projectId"`
	ID            string `form:"id" json:"id" binding:"required"`
	ToWarehouseID string `form:"toWarehouseId" json:"toWarehouseId"`
	Remark        string `form:"remark" json:"remark"`

	OperatorID   int64  `form:"operatorId" json:"operatorId"`
	OperatorName string `form:"operatorName" json:"operatorName"`
}

type TransferRejectReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	ID        string `form:"id" json:"id" binding:"required"`
	Reason    string `form:"reason" json:"reason" binding:"required"`

	OperatorID   int64  `form:"operatorId" json:"operatorId"`
	OperatorName string `form:"operatorName" json:"operatorName"`
}

type TransferCancelReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	ID        string `form:"id" json:"id" binding:"required"`
	Remark    string `form:"remark" json:"remark"`
}

type TransferStockOptionReq struct {
	ProjectID   string `form:"projectId" json:"projectId"`
	WarehouseID string `form:"warehouseId" json:"warehouseId" binding:"required"`
	Keyword     string `form:"keyword" json:"keyword"`

	Limit int `form:"limit" json:"limit"`
}

type ListTransferReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	Status    string `form:"status" json:"status"`
	Keyword   string `form:"keyword" json:"keyword"`
	Page      int    `form:"page" json:"page"`
	Size      int    `form:"size" json:"size"`
}

type GetTransferReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	ID        string `form:"id" json:"id" binding:"required"`
}

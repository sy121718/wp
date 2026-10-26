package inventorydto

// inventory_transfer_req.go — 调拨单的入参（迁移 630，W3）。

// TransferLineReq 一条调拨明细：搬源仓的哪条库存行、多少件。
//
// StockID 而不是 VariantID：调拨搬的是**源仓里那一批具体的货**，指向库存行才能表达
// 「无归属的货也能调拨」（迁移 624 的 variant_id IS NULL 行）。
type TransferLineReq struct {
	StockID  string `form:"stockId" json:"stockId" binding:"required"`
	Quantity int    `form:"quantity" json:"quantity"`
}

// CreateTransferReq 建调拨单：两个仓 + 明细 + 可选申请凭证。
//
// 两个仓都必填（与 ERP 的差异见 TransferService 的注释）；from <> to 由 DDL 兜底，
// 应用层先给可读的错误（不让运营看到一句 CHECK 约束原文）。
type CreateTransferReq struct {
	ProjectID       string            `form:"projectId" json:"projectId"`
	FromWarehouseID string            `form:"fromWarehouseId" json:"fromWarehouseId" binding:"required"`
	ToWarehouseID   string            `form:"toWarehouseId" json:"toWarehouseId" binding:"required"`
	Remark          string            `form:"remark" json:"remark"`
	OperatorID      int64             `form:"operatorId" json:"operatorId"`
	RequestID       string            `form:"requestId" json:"requestId"`
	Lines           []TransferLineReq `form:"lines" json:"lines" binding:"required"`
	// AttachmentIDs 申请凭证（可选）：有的工程要求先有申请单才建调拨。
	AttachmentIDs []int64 `form:"attachmentIds" json:"attachmentIds"`
}

// TransferShipReq 出货（**必须带出货凭证**）。
type TransferShipReq struct {
	ProjectID     string  `form:"projectId" json:"projectId"`
	ID            string  `form:"id" json:"id" binding:"required"`
	Remark        string  `form:"remark" json:"remark"`
	OperatorID    int64   `form:"operatorId" json:"operatorId"`
	AttachmentIDs []int64 `form:"attachmentIds" json:"attachmentIds"`
}

// TransferReceiveReq 签收（**必须带签收凭证**）。
type TransferReceiveReq struct {
	ProjectID     string  `form:"projectId" json:"projectId"`
	ID            string  `form:"id" json:"id" binding:"required"`
	Remark        string  `form:"remark" json:"remark"`
	OperatorID    int64   `form:"operatorId" json:"operatorId"`
	AttachmentIDs []int64 `form:"attachmentIds" json:"attachmentIds"`
}

// TransferCancelReq 取消（仅 pending）。
type TransferCancelReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	ID        string `form:"id" json:"id" binding:"required"`
	Remark    string `form:"remark" json:"remark"`
}

// ListTransferReq 调拨单列表（状态 / 关键词 + 分页）。
type ListTransferReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	Status    string `form:"status" json:"status"`
	Keyword   string `form:"keyword" json:"keyword"`
	Page      int    `form:"page" json:"page"`
	Size      int    `form:"size" json:"size"`
}

// GetTransferReq 调拨单详情。
type GetTransferReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	ID        string `form:"id" json:"id" binding:"required"`
}

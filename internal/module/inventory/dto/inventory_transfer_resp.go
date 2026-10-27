package inventorydto

// inventory_transfer_resp.go — 调拨单的出参（迁移 630，W3）。

// TransferRowResp 调拨单列表的一行（八个展示列 + 时间）。
//
// 「品类数 / 总数量」由 SQL 算好（model.TransferListRow），模板不做聚合。
type TransferRowResp struct {
	ID         string `json:"id"`
	TransferNo string `json:"transferNo"`
	// Status 四态（pending / in_transit / completed / cancelled）；
	// 展示文案由模板按 Status 取词（dto 不带展示字段，presentation_owner_test.go）。
	Status          string `json:"status"`
	Remark          string `json:"remark"`
	FromWarehouseID string `json:"fromWarehouseId"`
	FromCode        string `json:"fromCode"`
	FromName        string `json:"fromName"`
	ToWarehouseID   string `json:"toWarehouseId"`
	ToCode          string `json:"toCode"`
	ToName          string `json:"toName"`
	ItemCount       int    `json:"itemCount"`
	TotalQuantity   int    `json:"totalQuantity"`
	CreateTime      string `json:"createTime"`
	ShippedAt       string `json:"shippedAt"`
	ReceivedAt      string `json:"receivedAt"`
	// 审批链路（迁移 651）：列表要能看出「这张等谁批 / 谁驳回的」。
	RequestedAt    string `json:"requestedAt"`
	ApprovedAt     string `json:"approvedAt"`
	ApprovedName   string `json:"approvedName"`
	RejectedAt     string `json:"rejectedAt"`
	RejectedName   string `json:"rejectedName"`
	RejectedReason string `json:"rejectedReason"`
}

// TransferItemResp 一条调拨明细（含商品的展示信息，页面不必再查一次）。
type TransferItemResp struct {
	ID        int64  `json:"id"`
	StockID   string `json:"stockId"`
	VariantID string `json:"variantId"`
	SKUCode   string `json:"skuCode"`
	Quantity  int    `json:"quantity"`
	// ProductName / VariantLabel 行的**快照展示信息**（商品名与规格文本，
	// 与订单项同理：改名后历史单据仍显示当时的名字）。
	ProductName  string `json:"productName"`
	VariantLabel string `json:"variantLabel"`
}

// TransferAttachmentResp 一张凭证（含附件的展示信息）。
type TransferAttachmentResp struct {
	AttachmentID int64  `json:"attachmentId"`
	UsageType    string `json:"usageType"`
	FileName     string `json:"fileName"`
	FileSize     int64  `json:"fileSize"`
	MimeType     string `json:"mimeType"`
	URL          string `json:"url"`
}

// TransferResp 调拨单（详情与写动作回执共用：明细与凭证按需填充）。
type TransferResp struct {
	TransferRowResp
	Items       []*TransferItemResp       `json:"items,omitempty"`
	Attachments []*TransferAttachmentResp `json:"attachments,omitempty"`
	// 审批链路（迁移 651）：申请人 / 审批人 / 驳回人与时刻。
	// 只给 id 与时刻（姓名由页面经后台账号表补，dto 不带展示口径）。
	RequestedBy    int64  `json:"requestedBy"`
	RequestedAt    string `json:"requestedAt"`
	ApprovedBy     int64  `json:"approvedBy"`
	ApprovedAt     string `json:"approvedAt"`
	ApprovedName   string `json:"approvedName"`
	RejectedBy     int64  `json:"rejectedBy"`
	RejectedAt     string `json:"rejectedAt"`
	RejectedName   string `json:"rejectedName"`
	RejectedReason string `json:"rejectedReason"`
	// ShippedByName / ReceivedByName 操作人（写动作的审计字段）。
	CreateBy   int64 `json:"createBy"`
	ShippedBy  int64 `json:"shippedBy"`
	ReceivedBy int64 `json:"receivedBy"`
}

// TransferDetailResp 调拨单详情（三段凭证分开给，页面按用途分组渲染）。
type TransferDetailResp struct {
	Transfer *TransferResp `json:"transfer"`
	// RequestAttachments 申请凭证 / ShipAttachments 出货凭证 / ReceiveAttachments 签收凭证。
	RequestAttachments []*TransferAttachmentResp `json:"requestAttachments"`
	ShipAttachments    []*TransferAttachmentResp `json:"shipAttachments"`
	ReceiveAttachments []*TransferAttachmentResp `json:"receiveAttachments"`
	// AvailableStocks 建单表单的候选（源仓里还有可售的库存行）。
	AvailableStocks []*TransferStockOptionResp `json:"availableStocks,omitempty"`
}

// TransferStockOptionResp 建单调拨明细的候选：源仓里一条可售的库存行。
type TransferStockOptionResp struct {
	StockID string `json:"stockId"`
	// Name 货品名（迁移 642 的主档列）：只给编码，运营认不出「SZ_MY-TEE-1」是哪件衣服。
	Name      string `json:"name"`
	SKUCode   string `json:"skuCode"`
	VariantID string `json:"variantId"`
	// Quantity / ReservedQuantity / AvailableQuantity 这一行的事实数量（三个一起给）。
	Quantity          int `json:"quantity"`
	ReservedQuantity  int `json:"reservedQuantity"`
	AvailableQuantity int `json:"availableQuantity"`
}

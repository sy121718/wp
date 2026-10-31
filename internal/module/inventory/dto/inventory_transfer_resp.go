package inventorydto

type TransferRowResp struct {
	ID         string `json:"id"`
	TransferNo string `json:"transferNo"`

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

	RequestedAt    string `json:"requestedAt"`
	ApprovedAt     string `json:"approvedAt"`
	ApprovedName   string `json:"approvedName"`
	RejectedAt     string `json:"rejectedAt"`
	RejectedName   string `json:"rejectedName"`
	RejectedReason string `json:"rejectedReason"`
}

type TransferItemResp struct {
	ID        int64  `json:"id"`
	StockID   string `json:"stockId"`
	VariantID string `json:"variantId"`
	SKUCode   string `json:"skuCode"`
	Quantity  int    `json:"quantity"`

	ProductName  string `json:"productName"`
	VariantLabel string `json:"variantLabel"`
}

type TransferAttachmentResp struct {
	AttachmentID int64  `json:"attachmentId"`
	UsageType    string `json:"usageType"`
	FileName     string `json:"fileName"`
	FileSize     int64  `json:"fileSize"`
	MimeType     string `json:"mimeType"`
	URL          string `json:"url"`
}

type TransferResp struct {
	TransferRowResp
	Items       []*TransferItemResp       `json:"items,omitempty"`
	Attachments []*TransferAttachmentResp `json:"attachments,omitempty"`

	RequestedBy    int64  `json:"requestedBy"`
	RequestedAt    string `json:"requestedAt"`
	ApprovedBy     int64  `json:"approvedBy"`
	ApprovedAt     string `json:"approvedAt"`
	ApprovedName   string `json:"approvedName"`
	RejectedBy     int64  `json:"rejectedBy"`
	RejectedAt     string `json:"rejectedAt"`
	RejectedName   string `json:"rejectedName"`
	RejectedReason string `json:"rejectedReason"`

	CreateBy   int64 `json:"createBy"`
	ShippedBy  int64 `json:"shippedBy"`
	ReceivedBy int64 `json:"receivedBy"`
}

type TransferDetailResp struct {
	Transfer *TransferResp `json:"transfer"`

	RequestAttachments []*TransferAttachmentResp `json:"requestAttachments"`
	ShipAttachments    []*TransferAttachmentResp `json:"shipAttachments"`
	ReceiveAttachments []*TransferAttachmentResp `json:"receiveAttachments"`

	AvailableStocks []*TransferStockOptionResp `json:"availableStocks,omitempty"`
}

type TransferStockOptionResp struct {
	StockID string `json:"stockId"`

	Name      string `json:"name"`
	SKUCode   string `json:"skuCode"`
	VariantID string `json:"variantId"`

	Quantity          int `json:"quantity"`
	ReservedQuantity  int `json:"reservedQuantity"`
	AvailableQuantity int `json:"availableQuantity"`
}

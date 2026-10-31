package inventorydto

type StockVariantUsage struct {
	VariantID string
	SKUCode   string
}

type StockSKURowResp struct {
	ID            string
	WarehouseID   string
	WarehouseName string
	WarehouseCode string
	IsDefault     bool

	SKUCode     string
	ExternalSKU string
	Barcode     string
	Name        string
	ImageURL    string

	QuantityText      string
	ReservedQuantity  int
	AvailableQuantity int
	TrackQuantity     bool

	CostText    string
	FreightText string

	CurrencyText string

	FXText string

	FXMissing bool

	Status string

	VariantCount int

	Variants []StockVariantUsage

	Remark    string
	UpdatedAt string
}

type StockSKUPageReq struct {
	ProjectID   string `form:"project"`
	WarehouseID string `form:"warehouseId"`
	Keyword     string `form:"keyword"`

	Status string `form:"status"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

package productdto

type WarehouseOptionResp struct {
	ID        string
	Code      string
	Name      string
	IsDefault bool
}

type WarehouseSKUItemResp struct {
	SKUCode string

	ExternalSKU string

	StockID string

	Name     string
	ImageURL string

	Quantity      int
	TrackQuantity bool
}

type WarehouseSKUGroupResp struct {
	WarehouseID string

	WarehouseCode string
	WarehouseName string
	IsDefault     bool
	Items         []WarehouseSKUItemResp
}

type EntityTemplateOptionResp struct {
	ID   string
	Name string
}

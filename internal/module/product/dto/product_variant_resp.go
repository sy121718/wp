package productdto

import json "go_wp/pkg/jsonx"

type VariantOptionResp struct {
	VariantID   string   `json:"variantId"`
	SKUCode     string   `json:"skuCode"`
	ProductID   string   `json:"productId"`
	ProductName string   `json:"productName"`
	Price       float64  `json:"price"`
	CostPrice   *float64 `json:"costPrice"`
	Enabled     bool     `json:"enabled"`
}

type VariantListResp struct {
	VariantID    string          `json:"variantId"`
	SKUCode      string          `json:"skuCode"`
	OptionValues json.RawMessage `json:"optionValues"`

	Stocks []VariantStockRowResp `json:"stocks"`
}

type VariantStockRowResp struct {
	WarehouseID string `json:"warehouseId"`

	StockID string `json:"stockId"`

	SKUCode string `json:"skuCode"`

	Quantity int `json:"quantity"`

	TrackQuantity bool `json:"trackQuantity"`

	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`

	Name     string `json:"name"`
	ImageURL string `json:"imageUrl"`
}

type VariantStockGroupResp struct {
	WarehouseID   string
	WarehouseCode string
	WarehouseName string
	Rows          []VariantStockRowResp
}

type VariantPreviewResp struct {
	OptionValues json.RawMessage `json:"optionValues"`

	OptionKey string `json:"optionKey"`

	SKUCode string `json:"skuCode"`
}

type PreviewVariantResp struct {
	ProductID string `json:"productId"`

	Total int `json:"total"`

	Skipped int `json:"skipped"`

	Rows []*VariantPreviewResp `json:"rows"`
}

type SaveVariantListResp struct {
	ProductID string `json:"productId"`

	Created int `json:"created"`
	Updated int `json:"updated"`
	Deleted int `json:"deleted"`

	Variants []*VariantResp `json:"variants"`
}

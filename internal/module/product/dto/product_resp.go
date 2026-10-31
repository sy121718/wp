package productdto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

type ProductResp struct {
	Rating      *RatingResp     `json:"rating"`
	ID          string          `json:"id"`
	ProjectID   string          `json:"projectId"`
	Name        string          `json:"name"`
	Subtitle    string          `json:"subtitle"`
	Description json.RawMessage `json:"description"`
	Slug        string          `json:"slug"`

	SKUCode string `json:"skuCode"`
	Status  string `json:"status"`

	PublishedAt    utils.JSONTime `json:"publishedAt"`
	Sort           int            `json:"sort"`
	Unit           string         `json:"unit"`
	Weight         *float64       `json:"weight"`
	SEOTitle       string         `json:"seoTitle"`
	SEODescription string         `json:"seoDescription"`
	Images         []string       `json:"images"`

	ImageAlts []string `json:"imageAlts"`

	AttributeIDs []string `json:"attributeIds"`
	CategoryIDs  []string `json:"categoryIds"`

	PrimaryCategoryID string          `json:"primaryCategoryId"`
	TagIDs            []string        `json:"tagIds"`
	RelatedIDs        []string        `json:"relatedIds"`
	BrandID           string          `json:"brandId"`
	DefaultPrice      *float64        `json:"defaultPrice"`
	DefaultImage      string          `json:"defaultImage"`
	Metadata          json.RawMessage `json:"metadata"`

	PriceMin float64 `json:"priceMin"`
	PriceMax float64 `json:"priceMax"`

	VariantCount int            `json:"variantCount"`
	Variants     []*VariantResp `json:"variants,omitempty"`

	Attributes []*AttributeResp `json:"attributes,omitempty"`

	Categories []*CategoryResp `json:"categories,omitempty"`

	Brand *BrandResp `json:"brand,omitempty"`

	Tags      []*TagResp     `json:"tags,omitempty"`
	CreatedAt utils.JSONTime `json:"createdAt"`
	UpdatedAt utils.JSONTime `json:"updatedAt"`

	StockState string `json:"stockState"`
	StockTotal int    `json:"stockTotal"`

	StockUnknown bool `json:"stockUnknown,omitempty"`

	StockWarehouses []*ProductWarehouseStockResp `json:"stockWarehouses,omitempty"`
}

type ProductWarehouseStockResp struct {
	WarehouseID   string `json:"warehouseId"`
	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`

	SKUCode string `json:"skuCode"`

	State string `json:"state"`

	TrackQuantity bool `json:"trackQuantity"`
	Quantity      int  `json:"quantity"`

	CostPrice *float64 `json:"costPrice"`
}

type VariantResp struct {
	Stocks       []VariantStockGroupResp `json:"stocks"`
	StocksKnown  bool                    `json:"stocksKnown"`
	ID           string                  `json:"id"`
	ProductID    string                  `json:"productId"`
	SKUCode      string                  `json:"skuCode"`
	Barcode      string                  `json:"barcode"`
	Price        float64                 `json:"price"`
	ComparePrice *float64                `json:"comparePrice"`
	CostPrice    *float64                `json:"costPrice"`
	Image        string                  `json:"image"`
	OptionValues json.RawMessage         `json:"optionValues"`
	Enabled      bool                    `json:"enabled"`
	Sort         int                     `json:"sort"`
	StockTotal   int                     `json:"stockTotal"`
	CreatedAt    utils.JSONTime          `json:"createdAt"`
	UpdatedAt    utils.JSONTime          `json:"updatedAt"`
}

type GenerateVariantsResp struct {
	ProductID string `json:"productId"`

	Total int `json:"total"`

	Created int `json:"created"`

	Adopted int `json:"adopted"`

	Skipped int `json:"skipped"`

	Variants []*VariantResp `json:"variants"`
}

type ListResp struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Slug         string         `json:"slug"`
	Status       string         `json:"status"`
	Images       []string       `json:"images"`
	PriceMin     float64        `json:"priceMin"`
	PriceMax     float64        `json:"priceMax"`
	VariantCount int            `json:"variantCount"`
	UpdatedAt    utils.JSONTime `json:"updatedAt"`
}

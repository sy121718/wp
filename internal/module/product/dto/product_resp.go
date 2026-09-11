// product_resp.go — product 模块出参（service 返回）。
package productdto

import "encoding/json"

// ProductResp 商品详情（含变体列表）。
type ProductResp struct {
	ID             string          `json:"id"`
	ProjectID      string          `json:"projectId"`
	Name           string          `json:"name"`
	Subtitle       string          `json:"subtitle"`
	Description    json.RawMessage `json:"description"`
	Slug           string          `json:"slug"`
	Status         string          `json:"status"`
	Sort           int             `json:"sort"`
	Unit           string          `json:"unit"`
	Weight         *float64        `json:"weight"`
	SEOTitle       string          `json:"seoTitle"`
	SEODescription string          `json:"seoDescription"`
	Images         []string        `json:"images"`
	// AttributeIDs 商品引用的属性组 id（issue #7）。
	AttributeIDs   []string        `json:"attributeIds"`
	CategoryIDs    []string        `json:"categoryIds"`
	TagIDs         []string        `json:"tagIds"`
	RelatedIDs     []string        `json:"relatedIds"`
	BundleItems    json.RawMessage `json:"bundleItems"`
	BrandID        string          `json:"brandId"`
	DefaultPrice   *float64        `json:"defaultPrice"`
	DefaultImage   string          `json:"defaultImage"`
	Metadata       json.RawMessage `json:"metadata"`
	// PriceMin / PriceMax 是从变体派生的只读价格区间（商品主体不存价格）。
	PriceMin float64 `json:"priceMin"`
	PriceMax float64 `json:"priceMax"`
	// VariantCount 变体数量（单变体商品在前台不显示规格选择器）。
	VariantCount int            `json:"variantCount"`
	Variants     []*VariantResp `json:"variants,omitempty"`
	// Attributes 商品引用的属性组（含值）—— 同一属性组可被多个商品复用，
	// 这里返回的是共享定义本身，不是副本（issue #7）。
	Attributes []*AttributeResp `json:"attributes,omitempty"`
	CreatedAt    string         `json:"createdAt"`
	UpdatedAt    string         `json:"updatedAt"`
}

// VariantResp 变体。
type VariantResp struct {
	ID           string          `json:"id"`
	ProductID    string          `json:"productId"`
	SKUCode      string          `json:"skuCode"`
	Barcode      string          `json:"barcode"`
	Price        float64         `json:"price"`
	ComparePrice *float64        `json:"comparePrice"`
	CostPrice    *float64        `json:"costPrice"`
	Image        string          `json:"image"`
	OptionValues json.RawMessage `json:"optionValues"`
	Enabled      bool            `json:"enabled"`
	Sort         int             `json:"sort"`
	StockTotal   int             `json:"stockTotal"`
	CreatedAt    string          `json:"createdAt"`
	UpdatedAt    string          `json:"updatedAt"`
}

// ListResp 商品列表项（不含变体明细与 metadata —— metadata 默认查询不取）。
type ListResp struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Slug         string   `json:"slug"`
	Status       string   `json:"status"`
	Images       []string `json:"images"`
	PriceMin     float64  `json:"priceMin"`
	PriceMax     float64  `json:"priceMax"`
	VariantCount int      `json:"variantCount"`
	UpdatedAt    string   `json:"updatedAt"`
}

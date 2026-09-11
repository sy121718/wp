// product_req.go — product 模块入参（inbound 绑定用）。
package productdto

import "encoding/json"

// CreateReq 新建商品。
//
// 关联关系一律走 JSON 数组（图片 URL / 分类 id / 标签 id / 关联商品 id / 捆绑 BOM），
// 见 spec §数据结构精简原则：不为每类关系单开关联表。
type CreateReq struct {
	ProjectID      string          `json:"projectId"`
	Name           string          `json:"name" binding:"required"`
	Subtitle       string          `json:"subtitle"`
	Description    json.RawMessage `json:"description"`
	Slug           string          `json:"slug"`
	Unit           string          `json:"unit"`
	Weight         *float64        `json:"weight"`
	SEOTitle       string          `json:"seoTitle"`
	SEODescription string          `json:"seoDescription"`
	Images         []string        `json:"images"`
	// AttributeIDs 引用的属性组 id（issue #7）：只存引用，组与值的定义唯一一份，
	// 同一属性组可被多个商品复用。
	AttributeIDs   []string        `json:"attributeIds"`
	CategoryIDs    []string        `json:"categoryIds"`
	TagIDs         []string        `json:"tagIds"`
	RelatedIDs     []string        `json:"relatedIds"`
	BundleItems    json.RawMessage `json:"bundleItems"`
	BrandID        string          `json:"brandId"`
	DefaultPrice   *float64        `json:"defaultPrice"`
	DefaultImage   string          `json:"defaultImage"`
	Metadata       json.RawMessage `json:"metadata"`
}

// UpdateReq 修改商品（含 slug 改名；变体单独接口）。
type UpdateReq struct {
	ID             string          `json:"id" binding:"required"`
	Name           *string         `json:"name"`
	Subtitle       *string         `json:"subtitle"`
	Description    json.RawMessage `json:"description"`
	Slug           *string         `json:"slug"`
	Status         *string         `json:"status"`
	Unit           *string         `json:"unit"`
	Weight         *float64        `json:"weight"`
	SEOTitle       *string         `json:"seoTitle"`
	SEODescription *string         `json:"seoDescription"`
	Images         []string        `json:"images"`
	// AttributeIDs 引用的属性组 id（issue #7）；nil 表示本次不改引用，
	// 空数组表示解绑全部（与 CategoryIDs / TagIDs 同为「整体替换」语义）。
	AttributeIDs   []string        `json:"attributeIds"`
	CategoryIDs    []string        `json:"categoryIds"`
	TagIDs         []string        `json:"tagIds"`
	RelatedIDs     []string        `json:"relatedIds"`
	BundleItems    json.RawMessage `json:"bundleItems"`
	BrandID        *string         `json:"brandId"`
	DefaultPrice   *float64        `json:"defaultPrice"`
	DefaultImage   *string         `json:"defaultImage"`
	Metadata       json.RawMessage `json:"metadata"`
}

// GetReq 按 ID 查询。
type GetReq struct {
	ID string `form:"id" binding:"required"`
}

// ListReq 商品列表（分页 + 可选过滤）。
type ListReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
	Status    string `form:"status"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}

// DeleteReq 删除商品（连带其变体）。
type DeleteReq struct {
	ID string `json:"id" binding:"required"`
}

// CreateVariantReq 为商品新增一个变体。
//
// 未填字段由 service 逐字段判空后继承商品级默认值（空值以 NULL 判定，0 与 false 视为已填）。
type CreateVariantReq struct {
	ProductID    string          `json:"productId" binding:"required"`
	SKUCode      string          `json:"skuCode"`
	Barcode      string          `json:"barcode"`
	Price        *float64        `json:"price"`
	ComparePrice *float64        `json:"comparePrice"`
	CostPrice    *float64        `json:"costPrice"`
	Image        string          `json:"image"`
	OptionValues json.RawMessage `json:"optionValues"`
	Enabled      *bool           `json:"enabled"`
	Sort         int             `json:"sort"`
}

// UpdateVariantReq 修改变体（编辑路径不做默认值填充）。
type UpdateVariantReq struct {
	ID           string          `json:"id" binding:"required"`
	SKUCode      *string         `json:"skuCode"`
	Barcode      *string         `json:"barcode"`
	Price        *float64        `json:"price"`
	ComparePrice *float64        `json:"comparePrice"`
	CostPrice    *float64        `json:"costPrice"`
	Image        *string         `json:"image"`
	OptionValues json.RawMessage `json:"optionValues"`
	Enabled      *bool           `json:"enabled"`
	Sort         *int            `json:"sort"`
}

// DeleteVariantReq 删除变体。
type DeleteVariantReq struct {
	ID string `json:"id" binding:"required"`
}

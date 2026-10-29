package productdto

// PublicProductOptionsReq 查询指定工程内已上架商品的可选规格事实。
type PublicProductOptionsReq struct {
	ProjectID string `form:"projectId" binding:"required,uuid"`
	ProductID string `form:"productId" binding:"required,uuid"`
	Lang      string `form:"lang" binding:"max=35"`
}

type PublicProductOptionValue struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type PublicProductOption struct {
	Key    string                     `json:"key"`
	Name   string                     `json:"name"`
	Values []PublicProductOptionValue `json:"values"`
}

// PublicProductVariantOption 不含成本、条码、内部库存与后台备注；金额为整数分。
type PublicProductVariantOption struct {
	ID           string            `json:"id"`
	SKU          string            `json:"sku"`
	Options      map[string]string `json:"options"`
	Price        int64             `json:"price"`
	ComparePrice *int64            `json:"comparePrice"`
}

type PublicProductOptionsResp struct {
	ID       string                       `json:"id"`
	Name     string                       `json:"name"`
	Options  []PublicProductOption        `json:"options"`
	Variants []PublicProductVariantOption `json:"variants"`
}

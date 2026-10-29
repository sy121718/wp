package productdto

// ProductListResp 商品分页查询结果。Items 与商品 API 共用原对象。
type ProductListResp struct {
	Items []*ProductResp `json:"items"`
	Total int64          `json:"total"`
	Page  int            `json:"page"`
	Size  int            `json:"size"`
}

// EntityTemplateCatalogResp 实体类型可用的模板集合与默认绑定。
type EntityTemplateCatalogResp struct {
	Available bool
	Items     []EntityTemplateOptionResp
	DefaultID string
}

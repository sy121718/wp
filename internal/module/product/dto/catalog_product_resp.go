package productdto

type ProductListResp struct {
	Items []*ProductResp `json:"items"`
	Total int64          `json:"total"`
	Page  int            `json:"page"`
	Size  int            `json:"size"`
}

type EntityTemplateCatalogResp struct {
	Available bool
	Items     []EntityTemplateOptionResp
	DefaultID string
}

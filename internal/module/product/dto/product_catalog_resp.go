package productdto

// PublicCatalogReq 只接受数据筛选、排序和分页；展示配置保留在静态组件。
type PublicCatalogReq struct {
	ProjectID string `form:"projectId" binding:"required,uuid"`
	Query     string `form:"q" binding:"max=64"`
	Lang      string `form:"lang" binding:"max=35"`
	Filter    string `form:"filter" binding:"max=4000"`
	Sort      string `form:"sort" binding:"omitempty,oneof=default newest oldest priceAsc priceDesc ratingDesc"`
	Page      int    `form:"page" binding:"min=0,max=100"`
	PageSize  int    `form:"pageSize" binding:"min=0,max=60"`
}

// PublicCatalogItemResp 是已上线商品的公开事实；金额为整数分。
type PublicCatalogItemResp struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Subtitle     string   `json:"subtitle"`
	URL          string   `json:"url"`
	Images       []string `json:"images"`
	ImageAlt     string   `json:"imageAlt"`
	Price        *int64   `json:"price"`
	ComparePrice *int64   `json:"comparePrice"`
	MaxPrice     *int64   `json:"maxPrice"`
	Tags         []string `json:"tags"`
	Slug         string   `json:"slug"`
	Unit         string   `json:"unit"`
}

type PublicCatalogResp struct {
	Items    []PublicCatalogItemResp `json:"items"`
	Total    int64                   `json:"total"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"pageSize"`
}

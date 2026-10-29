package productdto

// TaxonomyListResp 是分页查询事实；Items 始终使用业务实体原有形状。
type TaxonomyListResp[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Size  int   `json:"size"`
}

// CategorySearchResp 按树根分页，Items 按先父后子的顺序返回当前森林中的原始分类。
// Total 是树根数量；MatchTotal 是匹配分类数量，两者不能互换。
type CategorySearchResp struct {
	Items      []*CategoryResp `json:"items"`
	Total      int64           `json:"total"`
	MatchTotal int64           `json:"matchTotal"`
	Page       int             `json:"page"`
	Size       int             `json:"size"`
}

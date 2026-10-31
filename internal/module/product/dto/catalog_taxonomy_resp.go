package productdto

type TaxonomyListResp[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Size  int   `json:"size"`
}

type CategorySearchResp struct {
	Items      []*CategoryResp `json:"items"`
	Total      int64           `json:"total"`
	MatchTotal int64           `json:"matchTotal"`
	Page       int             `json:"page"`
	Size       int             `json:"size"`
}

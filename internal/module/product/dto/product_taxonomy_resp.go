package productdto

import "go_wp/pkg/utils"

type CategoryResp struct {
	ID             string          `json:"id"`
	ProjectID      string          `json:"projectId"`
	ParentID       string          `json:"parentId"`
	Name           string          `json:"name"`
	Slug           string          `json:"slug"`
	Description    string          `json:"description"`
	Image          string          `json:"image"`
	SEOTitle       string          `json:"seoTitle"`
	SEODescription string          `json:"seoDescription"`
	Sort           int             `json:"sort"`
	Depth          int             `json:"depth"`
	HasChildren    bool            `json:"hasChildren,omitempty"`
	Matched        bool            `json:"matched,omitempty"`
	Children       []*CategoryResp `json:"children,omitempty"`
	CreatedAt      utils.JSONTime  `json:"createdAt"`
	UpdatedAt      utils.JSONTime  `json:"updatedAt"`
}

type BrandResp struct {
	ID             string         `json:"id"`
	ProjectID      string         `json:"projectId"`
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Logo           string         `json:"logo"`
	Description    string         `json:"description"`
	SEOTitle       string         `json:"seoTitle"`
	SEODescription string         `json:"seoDescription"`
	Sort           int            `json:"sort"`
	CreatedAt      utils.JSONTime `json:"createdAt"`
	UpdatedAt      utils.JSONTime `json:"updatedAt"`
	Matched        bool           `json:"matched,omitempty"`
}

type CategoryListResp struct {
	Items      []*CategoryResp `json:"items"`
	Total      int64           `json:"total"`
	MatchTotal int64           `json:"matchTotal,omitempty"`
}

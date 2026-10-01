// product_taxonomy_resp.go — 商品分类 / 品牌出参（issue #10，service 返回）。
package productdto

// CategoryResp 分类（列表接口返回**树**：顶级在数组里，子级挂在 Children）。
//
// Depth 由服务端在组装树时填好（顶级为 0），后台按它做缩进渲染 ——
// 前端不需要自己算层级，也不需要第二套父子规则。
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
	CreatedAt      string          `json:"createdAt"`
	UpdatedAt      string          `json:"updatedAt"`
}

// BrandResp 品牌。
type BrandResp struct {
	ID             string `json:"id"`
	ProjectID      string `json:"projectId"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Logo           string `json:"logo"`
	Description    string `json:"description"`
	SEOTitle       string `json:"seoTitle"`
	SEODescription string `json:"seoDescription"`
	Sort           int    `json:"sort"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
	Matched        bool   `json:"matched,omitempty"`
}

// CategoryPageResp 是后台分类树的分页读结果。
//
// Items 是**树**（顶级分类带 Children 嵌套）—— 列表一次渲染整棵树，不再逐层点进去。
// Total 是分页单位的数量：浏览态 = 顶级分类数，搜索态 = 命中所属的根分类数
// （搜索按根分页，否则同一棵树会跨页重复出现）。
// MatchTotal 只在搜索态有值：命中的分类条数（提示文案用它，分页不用）。
type CategoryPageResp struct {
	Items      []*CategoryResp `json:"items"`
	Total      int64           `json:"total"`
	MatchTotal int64           `json:"matchTotal,omitempty"`
}

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
}

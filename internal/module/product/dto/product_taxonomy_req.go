// product_taxonomy_req.go — 商品分类 / 品牌入参（issue #10，inbound 绑定用）。
//
// 分类与品牌都是工程内实体：slug 在工程内唯一，遵循 ContentTemplate 之前的成功
// 实践 —— 留空即由名称派生（中文名派生为空时由服务端兜底随机段）。
package productdto

// CreateCategoryReq 新建分类。ParentID 为空即顶级分类。
type CreateCategoryReq struct {
	ProjectID      string `json:"projectId"`
	ParentID       string `json:"parentId"`
	Name           string `json:"name" binding:"required"`
	Slug           string `json:"slug"`
	Description    string `json:"description"`
	Image          string `json:"image"`
	SEOTitle       string `json:"seoTitle"`
	SEODescription string `json:"seoDescription"`
	Sort           int    `json:"sort"`
}

// UpdateCategoryReq 修改分类。
//
// ParentID 语义与其它可选字段一致：nil 表示本次不改；指向空串表示**提升为顶级**。
type UpdateCategoryReq struct {
	ID             string  `json:"id" binding:"required"`
	ParentID       *string `json:"parentId"`
	Name           *string `json:"name"`
	Slug           *string `json:"slug"`
	Description    *string `json:"description"`
	Image          *string `json:"image"`
	SEOTitle       *string `json:"seoTitle"`
	SEODescription *string `json:"seoDescription"`
	Sort           *int    `json:"sort"`
}

// GetCategoryReq 按 ID 查询分类。
type GetCategoryReq struct {
	ID string `form:"id" binding:"required"`
}

// ListCategoryReq 分类列表（按工程过滤；返回树）。
type ListCategoryReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
}

// DeleteCategoryReq 删除分类（有子级或被商品引用时拒绝）。
type DeleteCategoryReq struct {
	ID string `json:"id" binding:"required"`
}

// CreateBrandReq 新建品牌。
type CreateBrandReq struct {
	ProjectID      string `json:"projectId"`
	Name           string `json:"name" binding:"required"`
	Slug           string `json:"slug"`
	Logo           string `json:"logo"`
	Description    string `json:"description"`
	SEOTitle       string `json:"seoTitle"`
	SEODescription string `json:"seoDescription"`
	Sort           int    `json:"sort"`
}

// UpdateBrandReq 修改品牌（可选字段为 nil 表示不变）。
type UpdateBrandReq struct {
	ID             string  `json:"id" binding:"required"`
	Name           *string `json:"name"`
	Slug           *string `json:"slug"`
	Logo           *string `json:"logo"`
	Description    *string `json:"description"`
	SEOTitle       *string `json:"seoTitle"`
	SEODescription *string `json:"seoDescription"`
	Sort           *int    `json:"sort"`
}

// GetBrandReq 按 ID 查询品牌。
type GetBrandReq struct {
	ID string `form:"id" binding:"required"`
}

// ListBrandReq 品牌列表（按工程过滤）。
type ListBrandReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
}

// DeleteBrandReq 删除品牌（被商品引用时拒绝）。
type DeleteBrandReq struct {
	ID string `json:"id" binding:"required"`
}

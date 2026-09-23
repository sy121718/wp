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
	ProjectID      string  `json:"projectId"`
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
	ID        string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
}

// ListCategoryReq 分类列表（按工程过滤；返回树）。
type ListCategoryReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
}

// DeleteCategoryReq 删除分类（有子级或被商品引用时拒绝）。
type DeleteCategoryReq struct {
	ID        string `json:"id" binding:"required"`
	ProjectID string `json:"projectId"`
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
	ProjectID      string  `json:"projectId"`
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
	ID        string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
}

// ListBrandReq 品牌列表（按工程过滤）。
//
// Page / Size 是**新增的可选分页字段**：两个都是零值时语义与加字段前一致 —— 取全部
//（集合源的品牌筛选选项、内容翻译的批量取数都走这条形态，它们要的是全量而不是第一页）。
// 后台品牌页显式给出两者，分页下推到 model 的 LIMIT/OFFSET（见 service 的 optionalPaging）。
type ListBrandReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
	// Page 从 1 开始（<=0 且 Size 也为 0 时不传分页）。
	Page int `form:"page"`
	// Size 每页条数（<=0 时用服务端默认；超过上限按上限截断，不由调用方决定）。
	Size int `form:"size"`
}

// DeleteBrandReq 删除品牌（被商品引用时拒绝）。
type DeleteBrandReq struct {
	ID        string `json:"id" binding:"required"`
	ProjectID string `json:"projectId"`
}

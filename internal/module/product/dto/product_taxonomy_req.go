package productdto

type CreateCategoryReq struct {
	ProjectID      string `json:"projectId" form:"projectId"`
	ParentID       string `json:"parentId" form:"parentId"`
	Name           string `json:"name" form:"name" binding:"required"`
	Slug           string `json:"slug" form:"slug"`
	Description    string `json:"description" form:"description"`
	Image          string `json:"image" form:"image"`
	SEOTitle       string `json:"seoTitle" form:"seoTitle"`
	SEODescription string `json:"seoDescription" form:"seoDescription"`
	Sort           int    `json:"sort" form:"sort"`
}

type UpdateCategoryReq struct {
	ID             string  `json:"id" form:"id" binding:"required"`
	ProjectID      string  `json:"projectId" form:"projectId"`
	ParentID       *string `json:"parentId" form:"parentId"`
	Name           *string `json:"name" form:"name"`
	Slug           *string `json:"slug" form:"slug"`
	Description    *string `json:"description" form:"description"`
	Image          *string `json:"image" form:"image"`
	SEOTitle       *string `json:"seoTitle" form:"seoTitle"`
	SEODescription *string `json:"seoDescription" form:"seoDescription"`
	Sort           *int    `json:"sort" form:"sort"`
}

type GetCategoryReq struct {
	ID        string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
}

type ListCategoryReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
}

type ListCategoryPageReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}

type DeleteCategoryReq struct {
	ID        string `json:"id" form:"id" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`
}

type CreateBrandReq struct {
	ProjectID      string `json:"projectId" form:"projectId"`
	Name           string `json:"name" form:"name" binding:"required"`
	Slug           string `json:"slug" form:"slug"`
	Logo           string `json:"logo" form:"logo"`
	Description    string `json:"description" form:"description"`
	SEOTitle       string `json:"seoTitle" form:"seoTitle"`
	SEODescription string `json:"seoDescription" form:"seoDescription"`
	Sort           int    `json:"sort" form:"sort"`
}

type UpdateBrandReq struct {
	ID             string  `json:"id" form:"id" binding:"required"`
	ProjectID      string  `json:"projectId" form:"projectId"`
	Name           *string `json:"name" form:"name"`
	Slug           *string `json:"slug" form:"slug"`
	Logo           *string `json:"logo" form:"logo"`
	Description    *string `json:"description" form:"description"`
	SEOTitle       *string `json:"seoTitle" form:"seoTitle"`
	SEODescription *string `json:"seoDescription" form:"seoDescription"`
	Sort           *int    `json:"sort" form:"sort"`
}

type GetBrandReq struct {
	ID        string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
}

type ListBrandReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`

	Page int `form:"page"`

	Size int `form:"size"`
}

type DeleteBrandReq struct {
	ID        string `json:"id" form:"id" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`
}

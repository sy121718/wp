// Package navigationdto 定义 navigation 模块请求/响应结构（0-C）。
// navigation 表示公开站点导航，与后台权限菜单 menu 严格隔离。
package navigationdto

// CreateReq 新建公开站点导航项。
type CreateReq struct {
	ProjectID string `json:"projectId" binding:"required"`
	Title     string `json:"title" binding:"required"`
	Path      string `json:"path" binding:"required"`
	Kind      string `json:"kind" binding:"required"`
	// ParentID 父导航项 ID（可选，为空表示根导航项）。
	ParentID *string `json:"parentId"`
	// SortOrder 排序权重（越小越靠前）。
	SortOrder int `json:"sortOrder"`
}

// UpdateReq 更新公开站点导航项（仅更新传入的非空字段）。
type UpdateReq struct {
	ID        string  `json:"id" binding:"required"`
	Title     *string `json:"title"`
	Path      *string `json:"path"`
	Kind      *string `json:"kind"`
	ParentID  *string `json:"parentId"`
	SortOrder *int    `json:"sortOrder"`
}

// GetReq 按 ID 查询导航项。
type GetReq struct {
	ID string `form:"id" binding:"required"`
}

// ListReq 按工程（可选 kind）列表。
type ListReq struct {
	ProjectID string `form:"projectId" binding:"required"`
	Kind      string `form:"kind"`
}

// DeleteReq 删除导航项。
type DeleteReq struct {
	ID string `json:"id" binding:"required"`
}

// MoveReq 调整导航项排序（可选：等价于仅改 SortOrder 的 Update，预留快捷入口）。
type MoveReq struct {
	ID        string `json:"id" binding:"required"`
	SortOrder *int   `json:"sortOrder"`
}

// NavigationResp 导航项响应。
type NavigationResp struct {
	ID        string  `json:"id"`
	ProjectID string  `json:"projectId"`
	Title     string  `json:"title"`
	Path      string  `json:"path"`
	Kind      string  `json:"kind"`
	ParentID  *string `json:"parentId"`
	SortOrder int     `json:"sortOrder"`
	UpdatedAt string  `json:"updatedAt"`
}

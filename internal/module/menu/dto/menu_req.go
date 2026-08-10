// Package menudto 菜单模块数据传输对象。
package menudto

// TreeReq 菜单树查询。
type TreeReq struct {
	Status *int   `form:"status" json:"status"`
	Type   *int   `form:"type" json:"type"`
	Search string `form:"search" json:"search" binding:"omitempty,max=50" validate:"omitempty,max=50"`
}

// DetailReq 查询单个菜单详情。
type DetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

// CreateReq 新建菜单。
type CreateReq struct {
	PermissionCode string `json:"permission_code" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	Title          string `json:"title" binding:"required,max=50" validate:"required,max=50"`
	ParentID       uint64 `json:"parent_id"`
	Type           int    `json:"type" binding:"required,oneof=1 2 3 4 5" validate:"required,oneof=1 2 3 4 5"`
	Path           string `json:"path" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	Component      string `json:"component" binding:"omitempty,max=255" validate:"omitempty,max=255"`
	ExternalURL    string `json:"external_url" binding:"omitempty,max=300" validate:"omitempty,max=300"`
	Icon           string `json:"icon" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Status         int    `json:"status"`
	IsHidden       int    `json:"is_hidden"`
	IsPublic       int    `json:"is_public"`
	SortOrder      int    `json:"sort_order"`
	Remark         string `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// UpdateReq 更新菜单。
type UpdateReq struct {
	ID             uint64 `json:"id" binding:"required" validate:"required"`
	PermissionCode string `json:"permission_code" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	Title          string `json:"title" binding:"required,max=50" validate:"required,max=50"`
	ParentID       uint64 `json:"parent_id"`
	Type           int    `json:"type" binding:"required,oneof=1 2 3 4 5" validate:"required,oneof=1 2 3 4 5"`
	Path           string `json:"path" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	Component      string `json:"component" binding:"omitempty,max=255" validate:"omitempty,max=255"`
	ExternalURL    string `json:"external_url" binding:"omitempty,max=300" validate:"omitempty,max=300"`
	Icon           string `json:"icon" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Status         int    `json:"status"`
	IsHidden       int    `json:"is_hidden"`
	IsPublic       int    `json:"is_public"`
	SortOrder      int    `json:"sort_order"`
	Remark         string `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// DeleteReq 批量删除菜单。
type DeleteReq struct {
	IDs []uint64 `json:"ids" binding:"required,min=1" validate:"required,min=1"`
}
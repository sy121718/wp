package admindto

// MenuTreeReq 菜单树查询。
type MenuTreeReq struct {
	Status *int   `form:"status" json:"status"`
	Type   *int   `form:"type" json:"type"`
	Search string `form:"search" json:"search" binding:"omitempty,max=50" validate:"omitempty,max=50"`
}

// MenuDetailReq 查询单个菜单详情。
type MenuDetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

// MenuCreateReq 新建菜单。
type MenuCreateReq struct {
	PermissionCode string  `json:"permission_code" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	Title          string  `json:"title" binding:"required,max=50" validate:"required,max=50"`
	TitleKey       *string `json:"title_key" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	ParentID       uint64  `json:"parent_id"`
	Type           int     `json:"type" binding:"required,oneof=1 2 3 4 5" validate:"required,oneof=1 2 3 4 5"`
	Path           string  `json:"path" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	Component      string  `json:"component" binding:"omitempty,max=255" validate:"omitempty,max=255"`
	ExternalURL    string  `json:"external_url" binding:"omitempty,max=300" validate:"omitempty,max=300"`
	Icon           string  `json:"icon" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Status         int     `json:"status"`
	IsHidden       int     `json:"is_hidden"`
	IsPublic       int     `json:"is_public"`
	SortOrder      int     `json:"sort_order"`
	Remark         string  `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// MenuUpdateReq 更新菜单。
type MenuUpdateReq struct {
	ID             uint64  `json:"id" binding:"required" validate:"required"`
	PermissionCode string  `json:"permission_code" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	Title          string  `json:"title" binding:"required,max=50" validate:"required,max=50"`
	TitleKey       *string `json:"title_key" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	ParentID       uint64  `json:"parent_id"`
	Type           int     `json:"type" binding:"required,oneof=1 2 3 4 5" validate:"required,oneof=1 2 3 4 5"`
	Path           string  `json:"path" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	Component      string  `json:"component" binding:"omitempty,max=255" validate:"omitempty,max=255"`
	ExternalURL    string  `json:"external_url" binding:"omitempty,max=300" validate:"omitempty,max=300"`
	Icon           string  `json:"icon" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Status         int     `json:"status"`
	IsHidden       int     `json:"is_hidden"`
	IsPublic       int     `json:"is_public"`
	SortOrder      int     `json:"sort_order"`
	Remark         string  `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// MenuDeleteReq 批量删除菜单。
type MenuDeleteReq struct {
	IDs []uint64 `json:"ids" binding:"required,min=1" validate:"required,min=1"`
}

// MenuTreeNode 菜单树节点（前端渲染用）。
type MenuTreeNode struct {
	ID             uint64         `json:"id"`
	PermissionCode string         `json:"permission_code"`
	Title          string         `json:"title"`
	TitleKey       string         `json:"title_key,omitempty"`
	ParentID       uint64         `json:"parent_id"`
	Type           int            `json:"type"`
	Path           string         `json:"path"`
	Component      string         `json:"component"`
	ExternalURL    string         `json:"external_url"`
	Icon           string         `json:"icon"`
	Status         int            `json:"status"`
	IsHidden       int            `json:"is_hidden"`
	IsPublic       int            `json:"is_public"`
	IsSystem       int            `json:"is_system"`
	SortOrder      int            `json:"sort_order"`
	Remark         string         `json:"remark"`
	Children       []MenuTreeNode `json:"children,omitempty"`
}

// MenuDetailResp 菜单详情。
type MenuDetailResp struct {
	ID             uint64  `json:"id"`
	PermissionCode string  `json:"permission_code"`
	Title          string  `json:"title"`
	TitleKey       *string `json:"title_key,omitempty"`
	ParentID       uint64  `json:"parent_id"`
	Type           int     `json:"type"`
	Path           string  `json:"path"`
	Component      string  `json:"component"`
	ExternalURL    string  `json:"external_url"`
	Icon           string  `json:"icon"`
	Status         int     `json:"status"`
	IsHidden       int     `json:"is_hidden"`
	IsPublic       int     `json:"is_public"`
	IsSystem       int     `json:"is_system"`
	SortOrder      int     `json:"sort_order"`
	Remark         string  `json:"remark"`
}

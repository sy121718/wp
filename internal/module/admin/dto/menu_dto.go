package admindto

// MenuTreeReq 菜单树查询。
type MenuTreeReq struct {
	Status *int   `form:"status" json:"status"`
	Type   *int   `form:"type" json:"type"`
	Search string `form:"search" json:"search" binding:"omitempty,max=50" validate:"omitempty,max=50"`
}

// MenuPageRow is a flat page row; ParentTitle explains relationships across pages.
type MenuPageRow struct {
	ID          uint64
	Title       string
	Path        string
	Type        int
	ParentID    uint64
	ParentTitle string
	Status      int
	SortOrder   int
	Remark      string
	Icon        string
	// PermissionCodes 是该菜单挂的全部权限码（迁移 470 起多对多）。
	// 列表页据此显示「这个节点代表哪些权限」；空切片表示没绑（目录 / iframe / 外链是常态）。
	PermissionCodes []string
}

// MenuParentChoice is an unpaged selector option.
type MenuParentChoice struct {
	ID     uint64
	Title  string
	Indent string
	Type   int
}

// MenuPageResp separates page rows from the complete parent selector.
type MenuPageResp struct {
	Total   int64
	Rows    []MenuPageRow
	Parents []MenuParentChoice
}

// MenuDetailReq 查询单个菜单详情。
type MenuDetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

// MenuCreateReq 新建菜单。
type MenuCreateReq struct {
	// PermissionCodes 是提交的全部权限码（迁移 470 起多对多，空值由 service 侧丢弃）。
	// 菜单（type=2）与按钮（type=3）至少一个，目录 / iframe / 外链必须为空 —— 判据在
	// validatePermissionBinding，这里只做长度上限（单个码 100，最多 50 个）。
	PermissionCodes []string `json:"permission_codes" binding:"max=50,dive,max=100" validate:"max=50,dive,max=100"`
	Title           string   `json:"title" binding:"required,max=50" validate:"required,max=50"`
	TitleKey        *string  `json:"title_key" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	ParentID        uint64   `json:"parent_id"`
	Type            int      `json:"type" binding:"required,oneof=1 2 3 4 5" validate:"required,oneof=1 2 3 4 5"`
	Path            string   `json:"path" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	Component       string   `json:"component" binding:"omitempty,max=255" validate:"omitempty,max=255"`
	ExternalURL     string   `json:"external_url" binding:"omitempty,max=300" validate:"omitempty,max=300"`
	Icon            string   `json:"icon" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Status          int      `json:"status"`
	IsHidden        int      `json:"is_hidden"`
	IsPublic        int      `json:"is_public"`
	SortOrder       int      `json:"sort_order"`
	Remark          string   `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// MenuUpdateReq 更新菜单。
type MenuUpdateReq struct {
	ID uint64 `json:"id" binding:"required" validate:"required"`
	// 全量替换语义：提交的集合就是该菜单的完整权限集合，未列出的会被撤销。
	PermissionCodes []string `json:"permission_codes" binding:"max=50,dive,max=100" validate:"max=50,dive,max=100"`
	Title           string   `json:"title" binding:"required,max=50" validate:"required,max=50"`
	TitleKey        *string  `json:"title_key" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	ParentID        uint64   `json:"parent_id"`
	Type            int      `json:"type" binding:"required,oneof=1 2 3 4 5" validate:"required,oneof=1 2 3 4 5"`
	Path            string   `json:"path" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	Component       string   `json:"component" binding:"omitempty,max=255" validate:"omitempty,max=255"`
	ExternalURL     string   `json:"external_url" binding:"omitempty,max=300" validate:"omitempty,max=300"`
	Icon            string   `json:"icon" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Status          int      `json:"status"`
	IsHidden        int      `json:"is_hidden"`
	IsPublic        int      `json:"is_public"`
	SortOrder       int      `json:"sort_order"`
	Remark          string   `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// MenuDeleteReq 批量删除菜单。
type MenuDeleteReq struct {
	IDs []uint64 `json:"ids" binding:"required,min=1" validate:"required,min=1"`
}

// MenuTreeNode 菜单树节点（前端渲染用）。
type MenuTreeNode struct {
	ID              uint64         `json:"id"`
	PermissionCodes []string       `json:"permission_codes"`
	Title           string         `json:"title"`
	TitleKey        string         `json:"title_key,omitempty"`
	ParentID        uint64         `json:"parent_id"`
	Type            int            `json:"type"`
	Path            string         `json:"path"`
	Component       string         `json:"component"`
	ExternalURL     string         `json:"external_url"`
	Icon            string         `json:"icon"`
	Status          int            `json:"status"`
	IsHidden        int            `json:"is_hidden"`
	IsPublic        int            `json:"is_public"`
	IsSystem        int            `json:"is_system"`
	SortOrder       int            `json:"sort_order"`
	Remark          string         `json:"remark"`
	Children        []MenuTreeNode `json:"children,omitempty"`
}

// MenuDetailResp 菜单详情。
type MenuDetailResp struct {
	ID              uint64   `json:"id"`
	PermissionCodes []string `json:"permission_codes"`
	Title           string   `json:"title"`
	TitleKey        *string  `json:"title_key,omitempty"`
	ParentID        uint64   `json:"parent_id"`
	Type            int      `json:"type"`
	Path            string   `json:"path"`
	Component       string   `json:"component"`
	ExternalURL     string   `json:"external_url"`
	Icon            string   `json:"icon"`
	Status          int      `json:"status"`
	IsHidden        int      `json:"is_hidden"`
	IsPublic        int      `json:"is_public"`
	IsSystem        int      `json:"is_system"`
	SortOrder       int      `json:"sort_order"`
	Remark          string   `json:"remark"`
}

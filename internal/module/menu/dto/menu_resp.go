// Package menudto 菜单模块数据传输对象。
package menudto

// TreeNode 菜单树节点（前端渲染用）。
type TreeNode struct {
	ID             uint64     `json:"id"`
	PermissionCode string     `json:"permission_code"`
	Title          string     `json:"title"`
	ParentID       uint64     `json:"parent_id"`
	Type           int        `json:"type"`
	Path           string     `json:"path"`
	Component      string     `json:"component"`
	ExternalURL    string     `json:"external_url"`
	Icon           string     `json:"icon"`
	Status         int        `json:"status"`
	IsHidden       int        `json:"is_hidden"`
	IsPublic       int        `json:"is_public"`
	IsSystem       int        `json:"is_system"`
	SortOrder      int        `json:"sort_order"`
	Remark         string     `json:"remark"`
	Children       []TreeNode `json:"children,omitempty"`
}

// DetailResp 菜单详情。
type DetailResp struct {
	ID             uint64 `json:"id"`
	PermissionCode string `json:"permission_code"`
	Title          string `json:"title"`
	ParentID       uint64 `json:"parent_id"`
	Type           int    `json:"type"`
	Path           string `json:"path"`
	Component      string `json:"component"`
	ExternalURL    string `json:"external_url"`
	Icon           string `json:"icon"`
	Status         int    `json:"status"`
	IsHidden       int    `json:"is_hidden"`
	IsPublic       int    `json:"is_public"`
	IsSystem       int    `json:"is_system"`
	SortOrder      int    `json:"sort_order"`
	Remark         string `json:"remark"`
}
// Package roledto 角色模块数据传输对象。
package roledto

// RoleItem 角色列表项。
type RoleItem struct {
	ID        uint64 `json:"id"`
	RoleCode  string `json:"role_code"`
	RoleName  string `json:"role_name"`
	Status    int    `json:"status"`
	IsSystem  int    `json:"is_system"`
	SortOrder int    `json:"sort_order"`
	Remark    string `json:"remark"`
}

// ListResp 列表响应。
type ListResp struct {
	Total int64      `json:"total"`
	List  []RoleItem `json:"list"`
}

// DetailResp 角色详情。
type DetailResp struct {
	ID        uint64 `json:"id"`
	RoleCode  string `json:"role_code"`
	RoleName  string `json:"role_name"`
	Status    int    `json:"status"`
	IsSystem  int    `json:"is_system"`
	SortOrder int    `json:"sort_order"`
	Remark    string `json:"remark"`
}

// MenuListResp 角色拥有的菜单 ID 列表。
type MenuListResp struct {
	MenuIDs []uint64 `json:"menu_ids"`
}

// MenuSaveResp 保存角色菜单响应。
type MenuSaveResp struct {
	RoleID uint64 `json:"role_id"`
}

// UserItem 角色下的用户简要信息。
type UserItem struct {
	ID       uint64 `json:"id"`
	Username string `json:"username,omitempty"`
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
	Status   int    `json:"status,omitempty"`
}

// UserListResp 角色下的用户列表。
type UserListResp struct {
	Total int64      `json:"total"`
	List  []UserItem `json:"list"`
}

// UserSaveResp 保存角色用户响应。
type UserSaveResp struct {
	RoleID uint64 `json:"role_id"`
}
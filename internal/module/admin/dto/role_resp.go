package admindto

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

// RoleListResp 列表响应。
type RoleListResp struct {
	Total int64      `json:"total"`
	List  []RoleItem `json:"list"`
}

// RoleDetailResp 角色详情。
type RoleDetailResp struct {
	ID        uint64 `json:"id"`
	RoleCode  string `json:"role_code"`
	RoleName  string `json:"role_name"`
	Status    int    `json:"status"`
	IsSystem  int    `json:"is_system"`
	SortOrder int    `json:"sort_order"`
	Remark    string `json:"remark"`
}

// RoleMenuListResp 角色拥有的菜单 ID 列表。
type RoleMenuListResp struct {
	MenuIDs []uint64 `json:"menu_ids"`
}

// RoleMenuSaveResp 保存角色菜单响应。
type RoleMenuSaveResp struct {
	RoleID uint64 `json:"role_id"`
}

// RoleUserItem 角色下的用户简要信息。
type RoleUserItem struct {
	ID       uint64 `json:"id"`
	Username string `json:"username,omitempty"`
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
	Status   int    `json:"status,omitempty"`
}

// RoleUserListResp 角色下的用户列表。
type RoleUserListResp struct {
	Total int64          `json:"total"`
	List  []RoleUserItem `json:"list"`
}

// RoleUserSaveResp 保存角色用户响应。
type RoleUserSaveResp struct {
	RoleID uint64 `json:"role_id"`
}

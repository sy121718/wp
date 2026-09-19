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

// RoleMenuListResp 角色拥有的菜单 ID 列表（已向上补齐祖先，见 withAncestorMenuIDs）。
type RoleMenuListResp struct {
	MenuIDs []uint64 `json:"menu_ids"`
}

// RolePermissionTreeResp 角色权限分配视图：授权树 + 该角色当前勾选。
//
// Tree 是**可用于勾选**的完整菜单树（目录 / 菜单 / 按钮，含已被禁用的已勾选项），
// MenuIDs 是当前勾选集合（同样已向上补齐祖先）。两者由同一个 service 方法一次算出：
// 分成两个接口的话，两次调用之间菜单表被改动就会出现「树里没有、勾选里有」的错位。
type RolePermissionTreeResp struct {
	RoleID   uint64         `json:"role_id"`
	RoleCode string         `json:"role_code"`
	RoleName string         `json:"role_name"`
	MenuIDs  []uint64       `json:"menu_ids"`
	Tree     []MenuTreeNode `json:"tree"`
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

package admindto

type RoleItem struct {
	ID        uint64 `json:"id"`
	RoleCode  string `json:"role_code"`
	RoleName  string `json:"role_name"`
	Status    int    `json:"status"`
	IsSystem  int    `json:"is_system"`
	SortOrder int    `json:"sort_order"`
	Remark    string `json:"remark"`
}

type RoleListResp struct {
	Total int64      `json:"total"`
	List  []RoleItem `json:"list"`
}

type RoleDetailResp struct {
	ID        uint64 `json:"id"`
	RoleCode  string `json:"role_code"`
	RoleName  string `json:"role_name"`
	Status    int    `json:"status"`
	IsSystem  int    `json:"is_system"`
	SortOrder int    `json:"sort_order"`
	Remark    string `json:"remark"`
}

type RoleMenuListResp struct {
	MenuIDs []uint64 `json:"menu_ids"`
}

type RolePermissionTreeResp struct {
	RoleID   uint64         `json:"role_id"`
	RoleCode string         `json:"role_code"`
	RoleName string         `json:"role_name"`
	MenuIDs  []uint64       `json:"menu_ids"`
	Tree     []MenuTreeNode `json:"tree"`
}

type RoleMenuSaveResp struct {
	RoleID uint64 `json:"role_id"`
}

type RoleUserItem struct {
	ID       uint64 `json:"id"`
	Username string `json:"username,omitempty"`
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
	Status   int    `json:"status,omitempty"`
}

type RoleUserListResp struct {
	Total int64          `json:"total"`
	List  []RoleUserItem `json:"list"`
}

type RoleUserSaveResp struct {
	RoleID uint64 `json:"role_id"`
}

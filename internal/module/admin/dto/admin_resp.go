package admindto

import (
	"go_wp/pkg/utils"
)

type AdminListResp struct {
	Total int64       `json:"total"`
	List  []AdminItem `json:"list"`
}

type AdminItem struct {
	ID         uint64          `json:"id"`
	Username   string          `json:"username"`
	Name       *string         `json:"name"`
	Avatar     *string         `json:"avatar"`
	Email      *string         `json:"email"`
	Phone      *string         `json:"phone"`
	Status     int             `json:"status"`
	CreateTime *utils.JSONTime `json:"create_time"`
}

type AdminCreateResp struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
}

type AdminLoginResp struct {
	UserID     uint64 `json:"-"`
	Username   string `json:"-"`
	SessionID  string `json:"-"`
	IssuedAt   int64  `json:"-"`
	RememberMe bool   `json:"-"`
}

type AdminDetailResp struct {
	ID                uint64          `json:"id"`
	Username          string          `json:"username"`
	Name              string          `json:"name"`
	Avatar            string          `json:"avatar"`
	Email             string          `json:"email"`
	Phone             string          `json:"phone"`
	Status            int             `json:"status"`
	IsAdmin           int             `json:"is_admin"`
	Roles             []any           `json:"roles"`
	Menus             []any           `json:"menus"`
	RegisterIP        string          `json:"register_ip"`
	RegisterLocation  string          `json:"register_location"`
	LastLoginIP       string          `json:"last_login_ip"`
	LastLoginLocation string          `json:"last_login_location"`
	LastLoginTime     *utils.JSONTime `json:"last_login_time"`
	CreateBy          uint64          `json:"create_by"`
	CreateTime        *utils.JSONTime `json:"create_time"`
	Remark            string          `json:"remark"`
}

type AdminProfileResp struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Avatar   string `json:"avatar"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Status   int    `json:"status"`
	Remark   string `json:"remark"`
	Menus    []any  `json:"menus"`
}

type AdminEditResp struct {
	ID uint64 `json:"id"`
}

type AdminDeleteResp struct {
	DeletedCount int64 `json:"deleted_count"`
}

type AdminRoleListItem struct {
	RoleCode string `json:"role_code"`
	RoleName string `json:"role_name"`
	Status   int    `json:"status"`
	IsSystem int    `json:"is_system"`
}

type AdminRoleListResp struct {
	List []AdminRoleListItem `json:"list"`
}

type AdminRoleSaveResp struct {
	UserID uint64 `json:"user_id"`
}

type AdminMenuListResp struct {
	DirectMenuIDs    []uint64 `json:"direct_menu_ids"`
	EffectiveMenuIDs []uint64 `json:"effective_menu_ids"`
}

type AdminMenuSaveResp struct {
	UserID uint64 `json:"user_id"`
}

type AdminListByDeptIDResp struct {
	Total int64           `json:"total"`
	List  []DeptAdminItem `json:"list"`
}

type DeptAdminItem struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Status   int    `json:"status"`
}

type AdminCountByDeptIDResp struct {
	Count int64 `json:"count"`
}

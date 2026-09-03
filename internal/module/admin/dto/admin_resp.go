package admindto

import "time"

// AdminListResp 管理员列表查询响应
//
// 返回符合查询条件的总记录数和当前页的数据列表。
type AdminListResp struct {
	Total int64       `json:"total"` // 符合条件的总记录数，用于前端分页组件
	List  []AdminItem `json:"list"`  // 当前页的管理员数据列表
}

// AdminItem 列表项，只返回前端需要的字段，不暴露敏感/内部字段。
type AdminItem struct {
	ID         uint64     `json:"id"`
	Username   string     `json:"username"`
	Name       *string    `json:"name"`
	Avatar     *string    `json:"avatar"`
	Email      *string    `json:"email"`
	Phone      *string    `json:"phone"`
	Status     int        `json:"status"`      // 1启用 2禁用 3封禁
	CreateTime *time.Time `json:"create_time"` // 创建时间
}

// AdminCreateResp 管理员新增响应
type AdminCreateResp struct {
	ID       uint64 `json:"id"` // 新增的管理员 ID，表示添加成功
	Username string `json:"username"`
}

// AdminLoginResp 管理员登录结果。
//
// 认证走 Session + Cookie：服务端通过 Set-Cookie 下发认证会话，响应体不再返回 JWT token。
// 以下字段仅供 handler 组装 cookie session（通过 json:"-" 从响应体隐藏，不下发前端）。
type AdminLoginResp struct {
	UserID     uint64 `json:"-"`
	Username   string `json:"-"`
	SessionID  string `json:"-"`
	IssuedAt   int64  `json:"-"`
	RememberMe bool   `json:"-"`
}

// AdminDetailResp 管理员详情响应
//
// 一个管理员查看另一个管理员的详细信息。
// 不包含 password、login_failure_count 等内部安全字段，不含 update_by / update_time。
type AdminDetailResp struct {
	ID                uint64     `json:"id"`
	Username          string     `json:"username"`
	Name              string     `json:"name"`
	Avatar            string     `json:"avatar"`
	Email             string     `json:"email"`
	Phone             string     `json:"phone"`
	Status            int        `json:"status"`   // 1启用 2禁用 3封禁
	IsAdmin           int        `json:"is_admin"` // 是否超管
	Roles             []any      `json:"roles"`    // 角色列表（由 service 层组装）
	Menus             []any      `json:"menus"`    // 菜单列表（由 service 层组装）
	RegisterIP        string     `json:"register_ip"`
	RegisterLocation  string     `json:"register_location"`
	LastLoginIP       string     `json:"last_login_ip"`
	LastLoginLocation string     `json:"last_login_location"`
	LastLoginTime     *time.Time `json:"last_login_time"`
	CreateBy          uint64     `json:"create_by"`
	CreateTime        *time.Time `json:"create_time"`
	Remark            string     `json:"remark"`
}

// AdminProfileResp 当前登录用户信息响应（从 Redis 会话或数据库获取）。
type AdminProfileResp struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Avatar   string `json:"avatar"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Status   int    `json:"status"`
	Remark   string `json:"remark"` //备注
	Menus    []any  `json:"menus"`
}

// AdminEditResp 修改管理员响应
type AdminEditResp struct {
	ID uint64 `json:"id"` // 新增的管理员 ID，表示编辑成功
}

// AdminDeleteResp 删除管理员响应
type AdminDeleteResp struct {
	DeletedCount int64 `json:"deleted_count"`
}

// AdminRoleListItem 用户绑定的角色项。
type AdminRoleListItem struct {
	RoleCode string `json:"role_code"`
	RoleName string `json:"role_name"`
	Status   int    `json:"status"`
	IsSystem int    `json:"is_system"`
}

// AdminRoleListResp 用户绑定的角色列表。
type AdminRoleListResp struct {
	List []AdminRoleListItem `json:"list"`
}

// AdminRoleSaveResp 用户角色绑定保存响应。
type AdminRoleSaveResp struct {
	UserID uint64 `json:"user_id"`
}

// AdminMenuListResp 用户直接额外菜单查询响应。
type AdminMenuListResp struct {
	DirectMenuIDs    []uint64 `json:"direct_menu_ids"`    // 用户直接额外权限对应的 menu_ids
	EffectiveMenuIDs []uint64 `json:"effective_menu_ids"` // 含角色继承的全部 menu_ids
}

// AdminMenuSaveResp 用户直接额外权限保存响应。
type AdminMenuSaveResp struct {
	UserID uint64 `json:"user_id"`
}

// AdminRoutesResp 动态路由权限投影响应。
type AdminRoutesResp struct {
	Routes          []RouteNode `json:"routes"`           // 当前用户可见的动态路由树
	Roles           []string    `json:"roles"`            // 角色编码列表
	PermissionCodes []string    `json:"permission_codes"` // 有效权限编码列表
}

// AdminListByDeptIDResp 按部门查管理员列表响应。
type AdminListByDeptIDResp struct {
	Total int64           `json:"total"`
	List  []DeptAdminItem `json:"list"`
}

// DeptAdminItem 部门下管理员简项。
type DeptAdminItem struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Status   int    `json:"status"`
}

// AdminCountByDeptIDResp 按部门查管理员数量响应。
type AdminCountByDeptIDResp struct {
	Count int64 `json:"count"`
}

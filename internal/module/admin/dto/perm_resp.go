package admindto

import "time"

// PermItem 权限点列表项。
type PermItem struct {
	ID             uint64     `json:"id"`
	PermissionCode string     `json:"permission_code"`
	PermissionName string     `json:"permission_name"`
	Module         string     `json:"module"`
	APIPath        string     `json:"api_path"`
	APIMethod      string     `json:"api_method"`
	Status         int        `json:"status"`
	Remark         string     `json:"remark"`
	CreateTime     *time.Time `json:"create_time"`
}

// PermListResp 列表查询响应。
type PermListResp struct {
	Total int64      `json:"total"`
	List  []PermItem `json:"list"`
}

// PermDetailResp 权限点详情。
type PermDetailResp struct {
	ID             uint64     `json:"id"`
	PermissionCode string     `json:"permission_code"`
	PermissionName string     `json:"permission_name"`
	Module         string     `json:"module"`
	APIPath        string     `json:"api_path"`
	APIMethod      string     `json:"api_method"`
	Status         int        `json:"status"`
	Remark         string     `json:"remark"`
	CreateTime     *time.Time `json:"create_time"`
	UpdateTime     *time.Time `json:"update_time"`
}

// PermOptionItem 权限选项（供菜单表单使用）。
type PermOptionItem struct {
	ID             uint64 `json:"id"`
	PermissionCode string `json:"permission_code"`
	PermissionName string `json:"permission_name"`
	Module         string `json:"module"`
	APIPath        string `json:"api_path"`
	APIMethod      string `json:"api_method"`
}

// PermOptionsResp 启用权限选项列表。
type PermOptionsResp struct {
	List []PermOptionItem `json:"list"`
}

// PermCreateResp 新建响应。
type PermCreateResp struct {
	ID uint64 `json:"id"`
}

// PermUpdateResp 更新响应。
type PermUpdateResp struct {
	ID uint64 `json:"id"`
}

// PermDeleteResp 删除响应。
type PermDeleteResp struct {
	DeletedCount int64 `json:"deleted_count"`
}

// PermBrief 供其他模块使用的权限摘要（code → path/method）。
type PermBrief struct {
	PermissionCode string `json:"permission_code"`
	APIPath        string `json:"api_path"`
	APIMethod      string `json:"api_method"`
}

// PermListByCodesResp 按 code 批量查询的权限摘要。
type PermListByCodesResp struct {
	List []PermBrief `json:"list"`
}

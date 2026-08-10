package permissiondto

import "time"

// PermissionItem 权限点列表项。
type PermissionItem struct {
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

// ListResp 列表查询响应。
type ListResp struct {
	Total int64             `json:"total"`
	List  []PermissionItem  `json:"list"`
}

// DetailResp 权限点详情。
type DetailResp struct {
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

// OptionItem 权限选项（供菜单表单使用）。
type OptionItem struct {
	ID             uint64 `json:"id"`
	PermissionCode string `json:"permission_code"`
	PermissionName string `json:"permission_name"`
	Module         string `json:"module"`
	APIPath        string `json:"api_path"`
	APIMethod      string `json:"api_method"`
}

// OptionsResp 启用权限选项列表。
type OptionsResp struct {
	List []OptionItem `json:"list"`
}

// CreateResp 新建响应。
type CreateResp struct {
	ID uint64 `json:"id"`
}

// UpdateResp 更新响应。
type UpdateResp struct {
	ID uint64 `json:"id"`
}

// DeleteResp 删除响应。
type DeleteResp struct {
	DeletedCount int64 `json:"deleted_count"`
}

// PermissionBrief 供其他模块使用的权限摘要（code → path/method）。
type PermissionBrief struct {
	PermissionCode string `json:"permission_code"`
	APIPath        string `json:"api_path"`
	APIMethod      string `json:"api_method"`
}

// ListByCodesResp 按 code 批量查询的权限摘要。
type ListByCodesResp struct {
	List []PermissionBrief `json:"list"`
}
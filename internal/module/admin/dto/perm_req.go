package admindto

// PermPageReq 分页参数。
type PermPageReq struct {
	Page  int `form:"page" json:"page" binding:"omitempty,gte=1" validate:"omitempty,gte=1"`
	Limit int `form:"limit" json:"limit" binding:"omitempty,gte=1,lte=100" validate:"omitempty,gte=1,lte=100"`
}

func (r *PermPageReq) GetPage() int {
	if r.Page < 1 {
		return 1
	}
	return r.Page
}

func (r *PermPageReq) GetLimit() int {
	if r.Limit < 1 {
		return 10
	}
	if r.Limit > 100 {
		return 100
	}
	return r.Limit
}

// PermListReq 权限点列表查询。
type PermListReq struct {
	PermPageReq
	Module  string `form:"module" json:"module" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Code    string `form:"code" json:"code" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	APIPath string `form:"api_path" json:"api_path" binding:"omitempty,max=200" validate:"omitempty,max=200"`
	Status  *int   `form:"status" json:"status"`
}

// PermDetailReq 查询单个权限点详情。
type PermDetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

// PermOptionsReq 菜单表单使用的启用权限选项查询。
type PermOptionsReq struct {
	Module string `form:"module" json:"module" binding:"omitempty,max=50" validate:"omitempty,max=50"`
}

// PermCreateReq 新建权限点。
type PermCreateReq struct {
	PermissionCode string `json:"permission_code" binding:"required,max=100" validate:"required,max=100"`
	PermissionName string `json:"permission_name" binding:"required,max=100" validate:"required,max=100"`
	Module         string `json:"module" binding:"required,max=50" validate:"required,max=50"`
	APIPath        string `json:"api_path" binding:"required,max=200" validate:"required,max=200"`
	APIMethod      string `json:"api_method" binding:"required,oneof=GET POST" validate:"required,oneof=GET POST"`
	Status         int    `json:"status"`
	Remark         string `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// PermUpdateReq 更新权限点。
type PermUpdateReq struct {
	ID             uint64 `json:"id" binding:"required" validate:"required"`
	PermissionCode string `json:"permission_code" binding:"required,max=100" validate:"required,max=100"`
	PermissionName string `json:"permission_name" binding:"required,max=100" validate:"required,max=100"`
	Module         string `json:"module" binding:"required,max=50" validate:"required,max=50"`
	APIPath        string `json:"api_path" binding:"required,max=200" validate:"required,max=200"`
	APIMethod      string `json:"api_method" binding:"required,oneof=GET POST" validate:"required,oneof=GET POST"`
	Status         int    `json:"status"`
	Remark         string `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// PermDeleteReq 批量删除权限点。
type PermDeleteReq struct {
	IDs []uint64 `json:"ids" binding:"required,min=1" validate:"required,min=1"`
}

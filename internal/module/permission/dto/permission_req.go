// Package permissiondto 权限点模块数据传输对象。
package permissiondto

// PageReq 分页参数。
type PageReq struct {
	Page  int `form:"page" json:"page" binding:"omitempty,gte=1" validate:"omitempty,gte=1"`
	Limit int `form:"limit" json:"limit" binding:"omitempty,gte=1,lte=100" validate:"omitempty,gte=1,lte=100"`
}

func (r *PageReq) GetPage() int {
	if r.Page < 1 {
		return 1
	}
	return r.Page
}

func (r *PageReq) GetLimit() int {
	if r.Limit < 1 {
		return 10
	}
	if r.Limit > 100 {
		return 100
	}
	return r.Limit
}

// ListReq 权限点列表查询。
type ListReq struct {
	PageReq
	Module   string `form:"module" json:"module" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Code     string `form:"code" json:"code" binding:"omitempty,max=100" validate:"omitempty,max=100"`
	APIPath  string `form:"api_path" json:"api_path" binding:"omitempty,max=200" validate:"omitempty,max=200"`
	Status   *int   `form:"status" json:"status"`
}

// DetailReq 查询单个权限点详情。
type DetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

// OptionsReq 菜单表单使用的启用权限选项查询。
type OptionsReq struct {
	Module string `form:"module" json:"module" binding:"omitempty,max=50" validate:"omitempty,max=50"`
}

// CreateReq 新建权限点。
type CreateReq struct {
	PermissionCode string `json:"permission_code" binding:"required,max=100" validate:"required,max=100"`
	PermissionName string `json:"permission_name" binding:"required,max=100" validate:"required,max=100"`
	Module         string `json:"module" binding:"required,max=50" validate:"required,max=50"`
	APIPath        string `json:"api_path" binding:"required,max=200" validate:"required,max=200"`
	APIMethod      string `json:"api_method" binding:"required,oneof=GET POST" validate:"required,oneof=GET POST"`
	Status         int    `json:"status"`
	Remark         string `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// UpdateReq 更新权限点。
type UpdateReq struct {
	ID             uint64 `json:"id" binding:"required" validate:"required"`
	PermissionCode string `json:"permission_code" binding:"required,max=100" validate:"required,max=100"`
	PermissionName string `json:"permission_name" binding:"required,max=100" validate:"required,max=100"`
	Module         string `json:"module" binding:"required,max=50" validate:"required,max=50"`
	APIPath        string `json:"api_path" binding:"required,max=200" validate:"required,max=200"`
	APIMethod      string `json:"api_method" binding:"required,oneof=GET POST" validate:"required,oneof=GET POST"`
	Status         int    `json:"status"`
	Remark         string `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// DeleteReq 批量删除权限点。
type DeleteReq struct {
	IDs []uint64 `json:"ids" binding:"required,min=1" validate:"required,min=1"`
}
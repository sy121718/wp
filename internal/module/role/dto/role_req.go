// Package roledto role 模块数据传输对象。
package roledto

// PageReq 分页请求参数。
type PageReq struct {
	Page  int `form:"page" json:"page" binding:"omitempty,gte=1" validate:"omitempty,gte=1"`
	Limit int `form:"limit" json:"limit" binding:"omitempty,gte=1,lte=100" validate:"omitempty,gte=1,lte=100"`
}

// GetPage 获取页码，默认返回 1。
func (r *PageReq) GetPage() int {
	if r.Page < 1 {
		return 1
	}
	return r.Page
}

// GetLimit 获取每页条数，默认 10，上限 100。
func (r *PageReq) GetLimit() int {
	if r.Limit < 1 {
		return 10
	}
	if r.Limit > 100 {
		return 100
	}
	return r.Limit
}

// ListReq 角色列表查询。
type ListReq struct {
	PageReq
	Keyword string `form:"keyword" json:"keyword" binding:"omitempty,max=50" validate:"omitempty,max=50"`
}

// DetailReq 查询角色详情。
type DetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

// CreateReq 新建角色。
type CreateReq struct {
	RoleCode  string `json:"role_code" binding:"required,max=50" validate:"required,max=50"`
	RoleName  string `json:"role_name" binding:"required,max=100" validate:"required,max=100"`
	Status    int    `json:"status"`
	SortOrder int    `json:"sort_order"`
	Remark    string `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// UpdateReq 更新角色元信息。
type UpdateReq struct {
	ID        uint64 `json:"id" binding:"required" validate:"required"`
	RoleName  string `json:"role_name" binding:"required,max=100" validate:"required,max=100"`
	Status    int    `json:"status"`
	SortOrder int    `json:"sort_order"`
	Remark    string `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// DeleteReq 删除角色。
type DeleteReq struct {
	ID uint64 `json:"id" binding:"required" validate:"required"`
}

// MenuListReq 查询角色拥有的菜单 ID。
type MenuListReq struct {
	RoleID uint64 `form:"role_id" json:"role_id" binding:"required" validate:"required"`
}

// MenuSaveReq 保存角色菜单授权（全量替换）。
type MenuSaveReq struct {
	RoleID  uint64   `json:"role_id" binding:"required" validate:"required"`
	MenuIDs []uint64 `json:"menu_ids"`
}

// UserListReq 查询角色下的用户。
type UserListReq struct {
	RoleID uint64 `form:"role_id" json:"role_id" binding:"required" validate:"required"`
	PageReq
}

// UserSaveReq 保存角色用户绑定（全量替换）。
type UserSaveReq struct {
	RoleID  uint64   `json:"role_id" binding:"required" validate:"required"`
	UserIDs []uint64 `json:"user_ids"`
}

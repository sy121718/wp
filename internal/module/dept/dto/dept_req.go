// Package deptdto dept 模块数据传输对象。
package deptdto

// DetailReq 查询部门详情。
type DetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

// CreateReq 新建部门。
type CreateReq struct {
	ParentID  uint64  `json:"parent_id"`
	DeptName  string  `json:"dept_name" binding:"required,max=100" validate:"required,max=100"`
	DeptCode  string  `json:"dept_code" binding:"required,max=50" validate:"required,max=50"`
	LeaderID  *uint64 `json:"leader_id"`
	SortOrder int     `json:"sort_order"`
	Status    int     `json:"status"`
	Remark    string  `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// UpdateReq 更新部门。
type UpdateReq struct {
	ID        uint64  `json:"id" binding:"required" validate:"required"`
	ParentID  uint64  `json:"parent_id"`
	DeptName  string  `json:"dept_name" binding:"required,max=100" validate:"required,max=100"`
	DeptCode  string  `json:"dept_code" binding:"required,max=50" validate:"required,max=50"`
	LeaderID  *uint64 `json:"leader_id"`
	SortOrder int     `json:"sort_order"`
	Status    int     `json:"status"`
	Remark    string  `json:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// DeleteReq 删除部门。
type DeleteReq struct {
	ID uint64 `json:"id" binding:"required" validate:"required"`
}

// UserListReq 查询部门下用户。
type UserListReq struct {
	DeptID uint64 `form:"dept_id" json:"dept_id" binding:"required" validate:"required"`
	Page   int    `form:"page" json:"page"`
	Limit  int    `form:"limit" json:"limit"`
}

// GetPage 返回当前页码，最小值为 1。
func (r *UserListReq) GetPage() int {
	if r.Page < 1 {
		return 1
	}
	return r.Page
}

// GetLimit 返回每页条目数，限制在 1~100 之间，默认 10。
func (r *UserListReq) GetLimit() int {
	if r.Limit < 1 {
		return 10
	}
	if r.Limit > 100 {
		return 100
	}
	return r.Limit
}

// UserSaveReq 批量分配用户到部门。
type UserSaveReq struct {
	DeptID  uint64   `json:"dept_id" binding:"required" validate:"required"`
	UserIDs []uint64 `json:"user_ids"`
}
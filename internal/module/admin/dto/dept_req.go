package admindto

// DeptDetailReq 查询部门详情。
type DeptDetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

// DeptCreateReq 新建部门。
type DeptCreateReq struct {
	ParentID  uint64  `json:"parent_id" form:"parent_id"`
	DeptName  string  `json:"dept_name" form:"dept_name" binding:"required,max=100" validate:"required,max=100"`
	DeptCode  string  `json:"dept_code" form:"dept_code" binding:"required,max=50" validate:"required,max=50"`
	LeaderID  *uint64 `json:"leader_id" form:"leader_id"`
	SortOrder int     `json:"sort_order" form:"sort_order"`
	Status    int     `json:"status" form:"status"`
	Remark    string  `json:"remark" form:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// DeptUpdateReq 更新部门。
type DeptUpdateReq struct {
	ID        uint64  `json:"id" form:"id" binding:"required" validate:"required"`
	ParentID  uint64  `json:"parent_id" form:"parent_id"`
	DeptName  string  `json:"dept_name" form:"dept_name" binding:"required,max=100" validate:"required,max=100"`
	DeptCode  string  `json:"dept_code" form:"dept_code" binding:"required,max=50" validate:"required,max=50"`
	LeaderID  *uint64 `json:"leader_id" form:"leader_id"`
	SortOrder int     `json:"sort_order" form:"sort_order"`
	Status    int     `json:"status" form:"status"`
	Remark    string  `json:"remark" form:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

// DeptDeleteReq 删除部门。
type DeptDeleteReq struct {
	ID uint64 `json:"id" form:"id" binding:"required" validate:"required"`
}

// DeptUserListReq 查询部门下用户。
type DeptUserListReq struct {
	DeptID uint64 `form:"dept_id" json:"dept_id" binding:"required" validate:"required"`
	Page   int    `form:"page" json:"page"`
	Limit  int    `form:"limit" json:"limit"`
}

// GetPage 返回当前页码，最小值为 1。
func (r *DeptUserListReq) GetPage() int {
	if r.Page < 1 {
		return 1
	}
	return r.Page
}

// GetLimit 返回每页条目数，限制在 1~100 之间，默认 10。
func (r *DeptUserListReq) GetLimit() int {
	if r.Limit < 1 {
		return 10
	}
	if r.Limit > 100 {
		return 100
	}
	return r.Limit
}

// DeptUserSaveReq 批量分配用户到部门。
type DeptUserSaveReq struct {
	DeptID  uint64   `json:"dept_id" form:"dept_id" binding:"required" validate:"required"`
	UserIDs []uint64 `json:"user_ids" form:"user_ids"`
}

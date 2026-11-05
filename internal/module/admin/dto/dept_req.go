package admindto

type DeptDetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

type DeptCreateReq struct {
	ParentID  uint64  `json:"parent_id" form:"parent_id"`
	DeptName  string  `json:"dept_name" form:"dept_name" binding:"required,max=100" validate:"required,max=100"`
	DeptCode  string  `json:"dept_code" form:"dept_code" binding:"required,max=50" validate:"required,max=50"`
	LeaderID  *uint64 `json:"leader_id" form:"leader_id"`
	SortOrder int     `json:"sort_order" form:"sort_order"`
	Status    int     `json:"status" form:"status"`
	Remark    string  `json:"remark" form:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

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

type DeptDeleteReq struct {
	ID  uint64   `json:"id" form:"id"`
	IDs []uint64 `json:"ids" form:"ids"`
}

// IDList 返回本次要处理的主键集合。
//
// 单条删除传 ID，批量删除传 IDs；两者都传时以 IDs 为准。
// 这里只做请求字段归一，不含业务规则。
func (r *DeptDeleteReq) IDList() []uint64 {
	if len(r.IDs) > 0 {
		return r.IDs
	}
	if r.ID == 0 {
		return nil
	}
	return []uint64{r.ID}
}

type DeptUserListReq struct {
	DeptID uint64 `form:"dept_id" json:"dept_id" binding:"required" validate:"required"`
	Page   int    `form:"page" json:"page"`
	Limit  int    `form:"limit" json:"limit"`
}

func (r *DeptUserListReq) GetPage() int {
	if r.Page < 1 {
		return 1
	}
	return r.Page
}

func (r *DeptUserListReq) GetLimit() int {
	if r.Limit < 1 {
		return 10
	}
	if r.Limit > 100 {
		return 100
	}
	return r.Limit
}

type DeptUserSaveReq struct {
	DeptID  uint64   `json:"dept_id" form:"dept_id" binding:"required" validate:"required"`
	UserIDs []uint64 `json:"user_ids" form:"user_ids"`
}

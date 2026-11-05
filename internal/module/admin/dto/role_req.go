package admindto

type RolePageReq struct {
	Page  int `form:"page" json:"page" binding:"omitempty,gte=1" validate:"omitempty,gte=1"`
	Limit int `form:"limit" json:"limit" binding:"omitempty,gte=1,lte=100" validate:"omitempty,gte=1,lte=100"`
}

func (r *RolePageReq) GetPage() int {
	if r.Page < 1 {
		return 1
	}
	return r.Page
}

func (r *RolePageReq) GetLimit() int {
	if r.Limit < 1 {
		return 10
	}
	if r.Limit > 100 {
		return 100
	}
	return r.Limit
}

type RoleListReq struct {
	RolePageReq
	Keyword string `form:"keyword" json:"keyword" binding:"omitempty,max=50" validate:"omitempty,max=50"`
}

type RoleDetailReq struct {
	ID uint64 `form:"id" json:"id" binding:"required" validate:"required"`
}

type RoleCreateReq struct {
	RoleCode  string `json:"role_code" form:"role_code" binding:"required,max=50" validate:"required,max=50"`
	RoleName  string `json:"role_name" form:"role_name" binding:"required,max=100" validate:"required,max=100"`
	Status    *int   `json:"status" form:"status"`
	SortOrder int    `json:"sort_order" form:"sort_order"`
	Remark    string `json:"remark" form:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

type RoleUpdateReq struct {
	ID        uint64 `json:"id" form:"id" binding:"required" validate:"required"`
	RoleName  string `json:"role_name" form:"role_name" binding:"required,max=100" validate:"required,max=100"`
	Status    int    `json:"status" form:"status"`
	SortOrder int    `json:"sort_order" form:"sort_order"`
	Remark    string `json:"remark" form:"remark" binding:"omitempty,max=200" validate:"omitempty,max=200"`
}

type RoleDeleteReq struct {
	ID  uint64   `json:"id" form:"id"`
	IDs []uint64 `json:"ids" form:"ids"`
}

// IDList 返回本次要处理的主键集合。
//
// 单条删除传 ID，批量删除传 IDs；两者都传时以 IDs 为准。
// 这里只做请求字段归一，不含业务规则。
func (r *RoleDeleteReq) IDList() []uint64 {
	if len(r.IDs) > 0 {
		return r.IDs
	}
	if r.ID == 0 {
		return nil
	}
	return []uint64{r.ID}
}

type RoleMenuListReq struct {
	RoleID uint64 `form:"role_id" json:"role_id" binding:"required" validate:"required"`
}

type RoleMenuSaveReq struct {
	RoleID  uint64   `json:"role_id" form:"role_id" binding:"required" validate:"required"`
	MenuIDs []uint64 `json:"menu_ids" form:"menu_ids"`

	OperatorID uint64 `json:"-" form:"-"`
}

type RoleUserListReq struct {
	RoleID uint64 `form:"role_id" json:"role_id" binding:"required" validate:"required"`
	RolePageReq
}

type RoleUserSaveReq struct {
	RoleID  uint64   `json:"role_id" form:"role_id" binding:"required" validate:"required"`
	UserIDs []uint64 `json:"user_ids" form:"user_ids"`

	OperatorID uint64 `json:"-"`
}

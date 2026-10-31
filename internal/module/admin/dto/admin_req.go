package admindto

type AdminPageReq struct {
	Page  int `form:"page" json:"page" binding:"omitempty,gte=1" validate:"omitempty,gte=1"`
	Limit int `form:"limit" json:"limit" binding:"omitempty,gte=1,lte=100" validate:"omitempty,gte=1,lte=100"`
}

func (r *AdminPageReq) GetPage() int {
	if r.Page < 1 {
		return 1
	}
	return r.Page
}

func (r *AdminPageReq) GetLimit() int {
	if r.Limit < 1 {
		return 10
	}
	if r.Limit > 100 {
		return 100
	}
	return r.Limit
}

type AdminListReq struct {
	AdminPageReq
	Email     string `form:"email" json:"email" binding:"omitempty,email_strict,max=100" validate:"omitempty,email_strict,max=100"`
	Name      string `form:"name" json:"name" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Phone     string `form:"phone" json:"phone" binding:"omitempty,max=20" validate:"omitempty,max=20"`
	Status    *int   `form:"status" json:"status"`
	SortField string `form:"sort_field" json:"sort_field" binding:"omitempty,oneof=id name email phone status create_time" validate:"omitempty,oneof=id name email phone status create_time"`
	SortOrder string `form:"sort_order" json:"sort_order" binding:"omitempty,oneof=asc desc" validate:"omitempty,oneof=asc desc"`
}

type AdminCreateReq struct {
	Avatar   string `json:"avatar" form:"avatar" binding:"omitempty,max=255" validate:"omitempty,max=255"`
	Email    string `json:"email" form:"email"  binding:"required,email_strict,max=100" validate:"required,email_strict,max=100"`
	Username string `json:"username" form:"username" binding:"required,max=50" validate:"required,max=50"`
	Phone    string `json:"phone" form:"phone" binding:"omitempty,max=20" validate:"omitempty,max=20"`
	Password string `json:"password" form:"password" binding:"required,min=6,max=100" validate:"required,min=6,max=100"`
	Remark   string `from:"remark" json:"remark" form:"remark"`
}

type AdminLoginReq struct {
	Username   string `json:"username" form:"username" binding:"required" validate:"required"`
	Password   string `json:"password" form:"password" binding:"required" validate:"required"`
	CaptchaID  string `json:"captcha_id" form:"captcha_id" binding:"required" validate:"required"`
	Captcha    string `json:"captcha" form:"captcha" binding:"required" validate:"required"`
	RememberMe bool   `json:"remember_me" form:"remember_me"`
}

type AdminDetailReq struct {
	Id uint64 `json:"id" form:"id" binding:"required" validate:"required"`
}

type AdminEditReq struct {
	Id       uint64 `json:"id" form:"id" binding:"required" validate:"required"`
	Username string `json:"username" form:"username"`
	Phone    string `json:"phone" form:"phone" binding:"omitempty,max=20" validate:"omitempty,max=20"`
	Email    string `form:"email" json:"email" binding:"omitempty,email_strict,max=100" validate:"omitempty,email_strict,max=100"`
	Remark   string `from:"remark" json:"remark" form:"remark"`
}

type AdminDeleteReq struct {
	Id         []uint64 `json:"ids" form:"ids" binding:"required" validate:"required"`
	OperatorID uint64   `json:"-"`
}

type AdminListByDeptIDReq struct {
	DeptID uint64 `form:"dept_id" json:"dept_id" binding:"required" validate:"required"`
	AdminPageReq
}

type AdminBatchSetDeptIDReq struct {
	DeptID  uint64   `json:"dept_id" form:"dept_id" binding:"required" validate:"required"`
	UserIDs []uint64 `json:"user_ids" form:"user_ids"`
}

type AdminRoleListReq struct {
	UserID uint64 `form:"user_id" json:"user_id" binding:"required" validate:"required"`
}

type AdminRoleSaveReq struct {
	UserID    uint64   `json:"user_id" form:"user_id" binding:"required" validate:"required"`
	RoleCodes []string `json:"role_codes" form:"role_codes"`

	OperatorID uint64 `json:"-"`
}

type AdminMenuListReq struct {
	UserID uint64 `form:"user_id" json:"user_id" binding:"required" validate:"required"`
}

type AdminMenuSaveReq struct {
	UserID  uint64   `json:"user_id" form:"user_id" binding:"required" validate:"required"`
	MenuIDs []uint64 `json:"menu_ids" form:"menu_ids"`

	OperatorID uint64 `json:"-"`
}

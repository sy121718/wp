// Package admindto 管理员模块数据传输对象
//
// 该包定义了 admin 模块（管理员/角色/权限点/菜单/部门）在接口层与业务层之间传递的请求和响应数据结构。
// 每个结构体对应一个业务操作的输入输出格式，包含参数校验规则（binding/validate tag），
// 确保进入 service 层的数据是合法、完整的。
package admindto

// AdminPageReq 分页参数，所有列表接口共用。
// 前端不传时默认 page=1, limit=10。
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

// AdminListReq 管理员列表查询请求参数
//
// 支持分页查询和多条件模糊搜索，所有查询条件均为可选（omitempty），
// 当条件为空时不做过滤，返回全部数据。
//
// 字段说明：
//   - 分页参数由 AdminPageReq 提供
//   - Email / Name / Phone：各自按 LIKE 模糊匹配，分别有长度限制防 SQL 注入
//   - Status：按精确值过滤，nil 表示不过滤状态
//   - SortField / SortOrder：排序控制，SortField 限定可排序字段白名单
type AdminListReq struct {
	AdminPageReq
	Email     string `form:"email" json:"email" binding:"omitempty,email_strict,max=100" validate:"omitempty,email_strict,max=100"`
	Name      string `form:"name" json:"name" binding:"omitempty,max=50" validate:"omitempty,max=50"`
	Phone     string `form:"phone" json:"phone" binding:"omitempty,max=20" validate:"omitempty,max=20"`
	Status    *int   `form:"status" json:"status"`
	SortField string `form:"sort_field" json:"sort_field" binding:"omitempty,oneof=id name email phone status create_time" validate:"omitempty,oneof=id name email phone status create_time"`
	SortOrder string `form:"sort_order" json:"sort_order" binding:"omitempty,oneof=asc desc" validate:"omitempty,oneof=asc desc"`
}

// AdminCreateReq 管理员新增请求参数
//
// 新增管理员，所有必填字段均为 required。
type AdminCreateReq struct {
	Avatar   string `json:"avatar" binding:"omitempty,max=255" validate:"omitempty,max=255"`
	Email    string `json:"email"  binding:"required,email_strict,max=100" validate:"required,email_strict,max=100"` // 邮箱，必填
	Username string `json:"username" binding:"required,max=50" validate:"required,max=50"`                           // 用户名，必填
	Phone    string `json:"phone" binding:"omitempty,max=20" validate:"omitempty,max=20"`                            // 手机号，可选
	Password string `json:"password" binding:"required,min=6,max=100" validate:"required,min=6,max=100"`             // 密码，必填
	Remark   string `from:"remark" json:"remark"`                                                                    //备注

}

// AdminLoginReq 管理员登录请求参数
type AdminLoginReq struct {
	Username   string `json:"username" binding:"required" validate:"required"`   // 登录账号
	Password   string `json:"password" binding:"required" validate:"required"`   // 登录密码
	CaptchaID  string `json:"captcha_id" binding:"required" validate:"required"` // 验证码标识
	Captcha    string `json:"captcha" binding:"required" validate:"required"`    // 验证码
	RememberMe bool   `json:"remember_me"`                                       // 是否保持登录，true 时 token 过期时间为 7 天
}

// AdminDetailReq 查询管理员详情
type AdminDetailReq struct {
	Id uint64 `json:"id" form:"id" binding:"required" validate:"required"`
}

// AdminEditReq 修改管理员
type AdminEditReq struct {
	Id       uint64 `json:"id" binding:"required" validate:"required"`
	Username string `json:"username"`                                                     // 登录账号
	Phone    string `json:"phone" binding:"omitempty,max=20" validate:"omitempty,max=20"` // 手机号，可选
	Email    string `form:"email" json:"email" binding:"omitempty,email_strict,max=100" validate:"omitempty,email_strict,max=100"`
	Remark   string `from:"remark" json:"remark"` //备注
}

// AdminDeleteReq 批量删除管理员
type AdminDeleteReq struct {
	Id         []uint64 `json:"ids" binding:"required" validate:"required"`
	OperatorID uint64   `json:"-"`
}

// --- 部门相关 ---

// AdminListByDeptIDReq 按部门 ID 查管理员列表。
type AdminListByDeptIDReq struct {
	DeptID uint64 `form:"dept_id" json:"dept_id" binding:"required" validate:"required"`
	AdminPageReq
}

// AdminBatchSetDeptIDReq 批量设置用户部门。
type AdminBatchSetDeptIDReq struct {
	DeptID  uint64   `json:"dept_id" binding:"required" validate:"required"`
	UserIDs []uint64 `json:"user_ids"`
}

// --- 用户角色绑定 ---

// AdminRoleListReq 查询用户绑定的角色列表。
type AdminRoleListReq struct {
	UserID uint64 `form:"user_id" json:"user_id" binding:"required" validate:"required"`
}

// AdminRoleSaveReq 全量替换用户角色绑定。
type AdminRoleSaveReq struct {
	UserID    uint64   `json:"user_id" binding:"required" validate:"required"`
	RoleCodes []string `json:"role_codes"`
	// OperatorID 当前操作者（handler 从会话注入，禁止前端传入）。
	OperatorID uint64 `json:"-"`
}

// --- 用户直接额外权限 ---

// AdminMenuListReq 查询用户直接额外菜单。
type AdminMenuListReq struct {
	UserID uint64 `form:"user_id" json:"user_id" binding:"required" validate:"required"`
}

// AdminMenuSaveReq 全量替换用户直接额外权限。
type AdminMenuSaveReq struct {
	UserID  uint64   `json:"user_id" binding:"required" validate:"required"`
	MenuIDs []uint64 `json:"menu_ids"`
}

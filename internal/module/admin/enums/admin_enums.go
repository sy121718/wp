// Package adminenums admin 模块（管理员/角色/权限点/菜单/部门）的业务消息。
// 常量值 = sys_i18n 稳定资源 key；注释保留中文（内置默认值/开发期可读）。
package adminenums

// --- 通用 ---

const (
	MsgSuccess      = "msg_operation_success" // 操作成功
	MsgBadRequest   = "ErrInvalidParams"      // 请求参数错误（拼 ": "+err.Error() 使用）
	MsgUnauthorized = "ErrUnauthorized"       // 未登录或登录已过期
)

// --- 管理员 ---

const (
	ErrCaptchaExpired   = "ErrCaptchaExpired"         // 验证码错误或已过期
	ErrBadCredentials   = "ErrInvalidPassword"        // 用户名或密码错误
	ErrAccountLocked    = "ErrAccountLocked"          // 账号已被锁定，请 %s 后重试（带参，key|param 协议）
	ErrAccountDisabled  = "ErrAdminDisabled"          // 账号已被禁用
	ErrAdminNotFound    = "ErrAdminNotFound"          // 管理员不存在
	ErrSuperAdminExists = "ErrSuperAdminExists"       // 系统已存在超级管理员，不能重复创建
	ErrFieldProtected   = "ErrFieldProtected"         // 不允许外部修改（受保护字段）
	ErrEmailExists      = "ErrAdminEmailExists"       // 该邮箱已存在
	ErrUsernameExists   = "ErrAdminUsernameExists"    // 用户名已存在，请修改
	ErrPhoneExists      = "ErrAdminPhoneExists"       // 手机号码重复，请修改
	ErrUserNotFound     = "ErrUserNotFound"           // 用户不存在
	ErrDeleteSelf       = "ErrAdminDeleteSelf"        // 不能删除当前登录管理员
	ErrDeleteSuperAdmin = "ErrAdminDeleteSuperAdmin"  // 不能删除超级管理员
	MsgLogoutSuccess    = "msg_admin_logout_success"  // 退出成功
	MsgWrongUserType    = "ErrAdminInvalidUserIDType" // 用户ID类型错误
)

// --- 角色 ---

const (
	ErrRoleNotFound    = "ErrRoleNotFound"    // 角色不存在
	ErrRoleCodeExists  = "ErrRoleCodeExists"  // 角色编码已存在
	ErrRoleIsSystem    = "ErrRoleIsSystem"    // 系统内置角色不可删除
	ErrRoleCodeNumeric = "ErrRoleCodeNumeric" // 角色编码不能为纯数字
)

// --- 菜单 ---

const (
	ErrMenuNotFound        = "ErrMenuNotFound"        // 菜单不存在
	ErrMenuHasChildren     = "ErrMenuHasChildren"     // 该菜单下有子菜单，无法删除
	ErrMenuIsSystem        = "ErrMenuIsSystem"        // 系统内置菜单不可删除或修改类型
	ErrMenuCircle          = "ErrMenuCircle"          // 不能将菜单移动到自身或其子级下
	ErrCodeNotBindable     = "ErrCodeNotBindable"     // 目录、iframe 和外链不能绑定权限编码
	ErrCodeRequired        = "ErrCodeRequired"        // 菜单和按钮类型必须绑定权限编码
	ErrCodeNotEnabled      = "ErrCodeNotEnabled"      // 绑定的权限编码不存在或未启用
	ErrComponentRequired   = "ErrComponentRequired"   // 菜单类型必须填写组件路径
	ErrComponentNotAllowed = "ErrComponentNotAllowed" // 只有菜单类型可以填写组件路径
	ErrComponentInvalid    = "ErrComponentInvalid"    // 组件路径格式不正确（如 layout.base / view.xxx 或 /src/views/xxx/index.vue）
)

// --- 权限点 ---

const (
	ErrPermissionNotFound = "ErrPermissionNotFound" // 权限点不存在
	ErrCodeExists         = "ErrCodeExists"         // 权限编码已存在
	ErrCodeImmutable      = "ErrCodeImmutable"      // 权限编码创建后不可修改
	ErrInvalidMethod      = "ErrInvalidMethod"      // 请求方法只允许 GET 或 POST
	ErrPermissionAssigned = "ErrPermissionAssigned" // 该权限已分配，请先解除角色和用户授权
	ErrMenuReferenced     = "ErrMenuReferenced"     // 该权限被菜单引用，无法删除
)

// --- 部门 ---

const (
	ErrDeptNotFound    = "ErrDeptNotFound"    // 部门不存在
	ErrDeptHasChildren = "ErrDeptHasChildren" // 该部门下有子部门，无法删除
	ErrDeptHasUsers    = "ErrDeptHasUsers"    // 该部门下有用户，无法删除
	ErrDeptCircle      = "ErrDeptCircle"      // 不能将部门移动到自身或其子级下
	ErrDeptCodeExists  = "ErrDeptCodeExists"  // 部门编码已存在
)

// --- 数据权限规则 ---

const (
	ErrRuleNotFound      = "ErrRuleNotFound"      // 数据规则不存在
	ErrInvalidDomain     = "ErrInvalidDomain"     // 不支持的数据域
	ErrInvalidAssignment = "ErrInvalidAssignment" // 无效的数据规则分配目标
)
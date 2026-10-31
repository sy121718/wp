package adminenums

import "go_wp/pkg/facing"

const (
	MsgSuccess      = "msg_operation_success"
	MsgBadRequest   = "ErrInvalidParams"
	MsgUnauthorized = "ErrUnauthorized"

	ErrInternal = "ErrInternal"
)

const (
	ErrCaptchaExpired   = "ErrCaptchaExpired"
	ErrBadCredentials   = "ErrInvalidPassword"
	ErrAccountLocked    = "ErrAccountLocked"
	ErrAccountDisabled  = "ErrAdminDisabled"
	ErrAdminNotFound    = "ErrAdminNotFound"
	ErrSuperAdminExists = "ErrSuperAdminExists"
	ErrFieldProtected   = "ErrFieldProtected"
	ErrEmailExists      = "ErrAdminEmailExists"
	ErrUsernameExists   = "ErrAdminUsernameExists"
	ErrPhoneExists      = "ErrAdminPhoneExists"
	ErrUserNotFound     = "ErrUserNotFound"
	ErrDeleteSelf       = "ErrAdminDeleteSelf"
	ErrDeleteSuperAdmin = "ErrAdminDeleteSuperAdmin"
	ErrSuperAdminOnly   = "ErrSuperAdminOnly"
	MsgLogoutSuccess    = "msg_admin_logout_success"
	MsgWrongUserType    = "ErrAdminInvalidUserIDType"
)

const (
	ErrRoleNotFound    = "ErrRoleNotFound"
	ErrRoleCodeExists  = "ErrRoleCodeExists"
	ErrRoleIsSystem    = "ErrRoleIsSystem"
	ErrRoleCodeNumeric = "ErrRoleCodeNumeric"
)

const (
	ErrMenuNotFound        = "ErrMenuNotFound"
	ErrMenuHasChildren     = "ErrMenuHasChildren"
	ErrMenuIsSystem        = "ErrMenuIsSystem"
	ErrMenuCircle          = "ErrMenuCircle"
	ErrMenuParentNotFound  = "ErrMenuParentNotFound"
	ErrMenuParentMustBeDir = "ErrMenuParentMustBeDir"
	ErrMenuDepthExceeded   = "ErrMenuDepthExceeded"
	ErrCodeNotBindable     = "ErrCodeNotBindable"
	ErrCodeRequired        = "ErrCodeRequired"
	ErrCodeNotEnabled      = "ErrCodeNotEnabled"
)

const (
	ErrPermissionNotFound = "ErrPermissionNotFound"
	ErrCodeExists         = "ErrCodeExists"
	ErrCodeImmutable      = "ErrCodeImmutable"
	ErrInvalidMethod      = "ErrInvalidMethod"
	ErrPermissionAssigned = "ErrPermissionAssigned"
	ErrMenuReferenced     = "ErrMenuReferenced"
)

const (
	ErrDeptNotFound    = "ErrDeptNotFound"
	ErrDeptHasChildren = "ErrDeptHasChildren"
	ErrDeptHasUsers    = "ErrDeptHasUsers"
	ErrDeptCircle      = "ErrDeptCircle"
	ErrDeptCodeExists  = "ErrDeptCodeExists"
)

const (
	ErrRuleNotFound        = "ErrRuleNotFound"
	ErrInvalidDomain       = "ErrInvalidDomain"
	ErrInvalidAssignment   = "ErrInvalidAssignment"
	ErrRuleConfigInvalid   = "ErrRuleConfigInvalid"
	ErrRuleFieldNotAllowed = "ErrRuleFieldNotAllowed"
	ErrRuleOpNotAllowed    = "ErrRuleOpNotAllowed"
	ErrRuleLogicNotAllowed = "ErrRuleLogicNotAllowed"
)

const (
	ErrSortFieldInvalid     = "admin.err.sortFieldInvalid"
	ErrSortDirectionInvalid = "admin.err.sortDirectionInvalid"
)

const (
	ErrI18nKeyEmpty   = "admin.err.i18nKeyEmpty"
	ErrI18nLangEmpty  = "admin.err.i18nLangEmpty"
	ErrI18nValueEmpty = "admin.err.i18nValueEmpty"
)

const (
	BulkDoneKey = "admin.bulk.done"

	BulkPartialKey = "admin.bulk.partial"
)

const (
	BulkNounAdmin      = "admin.bulk.noun.admin"
	BulkNounRole       = "admin.bulk.noun.role"
	BulkNounPermission = "admin.bulk.noun.permission"
	BulkNounMenu       = "admin.bulk.noun.menu"
	BulkNounDept       = "admin.bulk.noun.dept"
	BulkNounDatarule   = "admin.bulk.noun.datarule"
)

const (
	BulkI18nNoneSelected = "admin.i18nBulk.noneSelected"
	BulkI18nAllDeleted   = "admin.i18nBulk.allDeleted"
	BulkI18nAllSkipped   = "admin.i18nBulk.allSkipped"
	BulkI18nPartial      = "admin.i18nBulk.partial"
)

const (
	DatarulesEditorFieldPlaceholder = "admin.datarules.editor.field_placeholder"
	DatarulesEditorFieldRequired    = "admin.datarules.editor.field_required"

	I18nTitle           = "admin.i18n.title"
	DatarulesBackToEdit = "admin.datarules.back_to_edit"

	ShellLoginTitle = "shell.login.title"

	DevLoginLoopbackOnly  = "admin.dev_login.loopback_only"
	DevLoginFailed        = "admin.dev_login.failed"
	DevLoginSessionFailed = "admin.dev_login.session_failed"
	DevLoginCSRFFailed    = "admin.dev_login.csrf_failed"
)

var errFallbacks = map[string]string{
	ErrSortFieldInvalid:     "无效的排序字段：请改用列表支持的排序字段",
	ErrSortDirectionInvalid: "无效的排序方向：只支持 asc / desc",
	ErrI18nKeyEmpty:         "词条 key 不能为空",
	ErrI18nLangEmpty:        "词条语言不能为空",
	ErrI18nValueEmpty:       "词条内容不能为空",
	ErrInternal:             "操作失败，请稍后重试",
	ErrCaptchaExpired:       "验证码错误或已过期",
	ErrBadCredentials:       "用户名或密码错误",
	ErrAccountLocked:        "账号已被锁定，请 %s 后重试",
	ErrAccountDisabled:      "管理员已被禁用",
	ErrAdminNotFound:        "管理员不存在",
	ErrSuperAdminExists:     "系统已存在超级管理员，不能重复创建",
	ErrFieldProtected:       "不允许外部修改",
	ErrEmailExists:          "该邮箱已存在",
	ErrUsernameExists:       "用户名已存在，请修改",
	ErrPhoneExists:          "手机号码重复，请修改",
	ErrUserNotFound:         "用户不存在",
	ErrDeleteSelf:           "不能删除当前登录管理员",
	ErrDeleteSuperAdmin:     "不能删除超级管理员",
	ErrSuperAdminOnly:       "无权操作超管账号（仅超管可操作超管账号/超管角色）",
	ErrRoleNotFound:         "角色不存在",
	ErrRoleCodeExists:       "角色编码已存在",
	ErrRoleIsSystem:         "系统内置角色不可删除",
	ErrRoleCodeNumeric:      "角色编码不能为纯数字",
	ErrMenuNotFound:         "菜单不存在",
	ErrMenuHasChildren:      "该菜单下有子菜单，无法删除",
	ErrMenuIsSystem:         "系统内置菜单不可删除或修改类型",
	ErrMenuCircle:           "不能将菜单移动到自身或其子级下",
	ErrMenuParentNotFound:   "父级菜单不存在",
	ErrMenuParentMustBeDir:  "菜单/外链的父级必须是目录",
	ErrMenuDepthExceeded:    "菜单层级超过上限（3 级）",
	ErrCodeNotBindable:      "目录、iframe 和外链不能绑定权限编码",
	ErrCodeRequired:         "菜单和按钮类型必须绑定权限编码",
	ErrCodeNotEnabled:       "绑定的权限编码不存在或未启用",
	ErrPermissionNotFound:   "权限点不存在",
	ErrCodeExists:           "权限编码已存在",
	ErrCodeImmutable:        "权限编码创建后不可修改",
	ErrInvalidMethod:        "请求方法只允许 GET 或 POST",
	ErrPermissionAssigned:   "该权限已分配，请先解除角色和用户授权",
	ErrMenuReferenced:       "该权限被菜单引用，无法删除",
	ErrDeptNotFound:         "部门不存在",
	ErrDeptHasChildren:      "该部门下有子部门，无法删除",
	ErrDeptHasUsers:         "该部门下有用户，无法删除",
	ErrDeptCircle:           "不能将部门移动到自身或其子级下",
	ErrDeptCodeExists:       "部门编码已存在",
	ErrRuleNotFound:         "数据规则不存在",
	ErrInvalidDomain:        "不支持的数据域",
	ErrInvalidAssignment:    "无效的数据规则分配目标",
	ErrRuleConfigInvalid:    "数据规则配置不合法",
	ErrRuleFieldNotAllowed:  "规则引用了数据域白名单之外的字段",
	ErrRuleOpNotAllowed:     "规则使用了该字段未声明的操作符",
	ErrRuleLogicNotAllowed:  "条件组合逻辑只允许 AND 或 OR",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }

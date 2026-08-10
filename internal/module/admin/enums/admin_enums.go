// Package adminenums admin 模块的业务消息与 i18n key。
//
// 当前阶段用硬编码中文常量占位。
// sys_i18n 表数据就绪后，将常量值替换为 i18n.GetText(key, lang) 调用即可。
package admin_enums

// --- 业务错误消息（service 层） ---

const (
	ErrCaptchaExpired   = "验证码错误或已过期"
	ErrBadCredentials   = "用户名或密码错误"
	ErrAccountLocked    = "账号已被锁定，请 %s 后重试"
	ErrAccountDisabled  = "账号已被禁用"
	ErrAdminNotFound    = "管理员不存在"
	ErrEmailExists      = "该邮箱已存在"
	ErrUsernameExists   = "用户名已存在，请修改"
	ErrPhoneExists      = "手机号码重复，请修改"
	ErrUserNotFound     = "用户不存在"
	ErrDeleteSelf       = "不能删除当前登录管理员"
	ErrDeleteSuperAdmin = "不能删除超级管理员"
)

// --- 响应消息（handler 层） ---

const (
	MsgSuccess       = "success" // 操作成功
	MsgLogoutSuccess = "退出成功"
	MsgBadRequest    = "请求参数错误" // 拼 +": "+err.Error() 使用
	MsgUnauthorized  = "未登录或登录已过期"
	MsgWrongUserType = "用户ID类型错误"
)

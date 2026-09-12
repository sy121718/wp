// Package userenums 用户模块的响应与错误消息（issue #36）。
package userenums

const (
	MsgRegisterSuccess = "注册成功，请查收验证邮件"
	MsgActivateSuccess = "邮箱验证成功"
)

const (
	ErrInvalidParam      = "参数不合法"
	ErrUsernameRequired  = "用户名不能为空"
	ErrUsernameTooShort  = "用户名至少 3 个字符"
	ErrUsernameTooLong   = "用户名最多 32 个字符"
	ErrUsernameTaken     = "用户名已被占用"
	ErrEmailRequired     = "邮箱不能为空"
	ErrEmailInvalid      = "邮箱格式不正确"
	ErrEmailTaken        = "邮箱已被注册"
	ErrPasswordTooShort  = "密码至少 8 个字符"
	ErrUserNotFound      = "用户不存在"
	ErrActivationInvalid = "验证链接无效或已过期"
	ErrMailUnavailable   = "验证邮件暂时发不出去，请稍后重试或联系管理员"
)

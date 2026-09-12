package userdto

// user_req.go — 用户模块请求（issue #36，当前为注册链路）。

// RegisterReq 注册。
//
// 注意这里**没有 role 字段**：用户侧的注册不接受调用方指定身份
// （注册即普通用户），身份只能由管理侧授予。
type RegisterReq struct {
	Username string
	Email    string
	Password string
	Nickname string
	// RegisterIP / RegisterLocation 由 inbound 覆盖写入，客户端不可伪造。
	RegisterIP       string
	RegisterLocation string
	// Locale 决定用哪套语言模板（空 = 通用模板）。
	Locale string
}

// ActivateEmailReq 邮箱验证。
type ActivateEmailReq struct {
	Key string
	// Locale 决定欢迎邮件用哪套语言模板（空 = 通用模板）。
	Locale string
}

// ResendActivationReq 重发验证邮件。
type ResendActivationReq struct {
	Email  string
	Locale string
}

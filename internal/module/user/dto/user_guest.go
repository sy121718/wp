package userdto

// user_guest.go — 访客下单自动开号的请求与响应。

// GuestAccountReq 为访客下单确保账号存在。
type GuestAccountReq struct {
	// Email 客户下单时填的邮箱（也是账号身份）。
	Email string
	// Name 客户名（用作昵称；为空时展示名退回用户名）。
	Name string
	// Locale 决定用哪套语言模板。
	Locale string
	// RegisterIP 由调用方从请求上下文取，客户端不可伪造。
	RegisterIP string
}

// GuestAccountResp 开号结果。
type GuestAccountResp struct {
	UserID   uint64
	Username string
	// Created 本次是否**新建**了账号；false 表示该邮箱已有账号，只做了关联。
	//
	// 这个字段是给调用方判断「要不要提示客户查收密码邮件」用的：
	// 已有账号时**不发密码**（见 service 的安全边界说明）。
	Created bool
	// PasswordMailed 初始密码邮件是否已受理（仅新建且发信成功时为真）。
	PasswordMailed bool
}

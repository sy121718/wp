package userdto

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

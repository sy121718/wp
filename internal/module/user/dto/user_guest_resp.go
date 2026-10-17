package userdto

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

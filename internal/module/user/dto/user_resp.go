package userdto

// user_resp.go — 用户模块响应（issue #36）。

// RegisterResp 注册结果。
//
// **不返回激活码 / 激活链接**：那是用户邮箱里的东西。
// 接口回传它等于让任何调用方都能直接激活，验证邮箱这一步就白做了。
type RegisterResp struct {
	UserID   uint64 `json:"userId"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Status   int    `json:"status"`
	// MailQueued 表示验证邮件已受理；false 时前端应提示「可点重发」。
	MailQueued bool `json:"mailQueued"`
}

// ActivateResp 邮箱验证结果。
type ActivateResp struct {
	UserID   uint64 `json:"userId"`
	Username string `json:"username"`
}

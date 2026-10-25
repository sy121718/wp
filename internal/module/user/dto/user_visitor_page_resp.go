package userdto

// user_visitor_page_resp.go — **访客页面**（登录 / 注册 / 找回 / 重置 / 消息 / 账号）的渲染视图。
//
// 与后台客户页的视图分开：访客模板用 `.["key"]` 链式 map 索引读数据
//（见 internal/templates/user/*.html），所以这些字段全部带 `view:"小写键"` 标签。
//
// 页壳键（csrf_token / user / lang / t / dir / site / title）**不在这里**：
// 它们是每一屏都一样的壳关切，由 Handle.render 统一摊平后注入 ——
// 让六个视图各声明一遍只会把「这一屏要什么业务数据」淹没在壳键里。
//
// 零值可用：这些页有大量失败分支（参数错、凭证错、服务不可用），
// 每个失败分支都渲染**同一张模板**，只是带不同的 error/message。

// LoginPageResp 登录页。
type LoginPageResp struct {
	Next    string `view:"next"`
	Account string `view:"account"`
	// Error 登录失败的提示（已取好词；空串 = 无错误）。
	Error string `view:"error"`
}

// RegisterPageResp 注册页。
type RegisterPageResp struct {
	// Form 回显的表单值（注册失败时保留用户输入）。
	//
	// 类型是 any 而不是结构体：访客模板用 `.["form"]["username"]` 链式索引，
	// 而 adminui.ViewMap 不递归摊平嵌套结构体 —— 调用方传**已摊平的 map** 进来。
	Form any `view:"form"`
	// Error 注册失败的提示。
	Error string `view:"error"`
}

// RegisterDonePageResp 注册完成页。
type RegisterDonePageResp struct {
	Email string `view:"email"`
	// MailQueued 初始密码是否已经寄出（false = 只完成注册，没有发信）。
	MailQueued bool `view:"mailQueued"`
}

// ForgotPageResp 忘记密码页。
type ForgotPageResp struct {
	Email string `view:"email"`
	Error string `view:"error"`
}

// ResetPageResp 重置密码页。
type ResetPageResp struct {
	// Key 重置链接里的令牌（模板回传到表单）。
	Key   string `view:"key"`
	Email string `view:"email"`
	Error string `view:"error"`
}

// MessagePageResp 访客页的**结论页**（成功 / 失败共用一张模板）。
//
// 它是「一句话 + 一个返回路径」的通用形态：注册完成、找回密码已发、账号操作失败……
// 都走它。所以定题与正文都是**已取好词的文本**（调用方取词，见 renderMessage）。
type MessagePageResp struct {
	// Title 页面标题（模板读 .title，也进 <title>）。
	//
	// 这里的 title 与壳键同名：壳键注入时用 `if _, ok := data["title"]; !ok` 判存在，
	// 所以视图给了就优先用视图的 —— 这正是消息页要的「定题随结论变」。
	Title   string `view:"title"`
	Message string `view:"message"`
	OK      bool   `view:"ok"`
}

// AccountPageResp 访客账号页（资料 + 设备列表）。
type AccountPageResp struct {
	Account  any `view:"account"`
	Sessions any `view:"sessions"`
	// ActionError 资料操作的失败提示（已取好词）。
	ActionError string `view:"actionError"`
}

// RegisterFormResp 注册表单的回显值（密码不回显）。
//
// 模板用 `.["form"]["username"]` 这类链式索引读它，所以字段名保持小写键语义
// （ViewMap 会把它摊成一个 map 值，模板的链式索引照旧）。
type RegisterFormResp struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
}

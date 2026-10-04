package aidto

// ai_chat.go — ai 模块的对话入口形状（inbound ↔ service）。
//
// 形状照 internal/module/CLAUDE.md 的约定：请求结构体只描述「调用方要什么」，
// 绑定逻辑在 handle 层，json 与 form tag 并存（路由只用 GET / POST，参数可走 body 或 Query）。

// ChatReq 一次最小对话请求。provider_key + model 二元组即「用了哪个第三方」的标识。
type ChatReq struct {
	ProviderKey     string `json:"provider_key" form:"provider_key" binding:"required,max=50"`
	Model           string `json:"model"        form:"model"        binding:"required,max=200"`
	Input           string `json:"input"        form:"input"        binding:"required,max=200000"`
	MaxOutputTokens int64  `json:"max_output_tokens" form:"max_output_tokens"`

	// SessionID / UserID 是这次调用的**归属**，只用于写调用流水（ai_call_log），不进出站请求。
	//
	// 两个都由服务端填，**不接受调用方自报**：会话层填自己正在续写的那条会话与发起人，
	// 对外接口把 UserID 覆盖成当前登录账号、SessionID 清零（否则日志里的「谁调用的」可被伪造）。
	// 0 表示未记录。
	SessionID int64 `json:"session_id" form:"session_id"`
	UserID    int64 `json:"user_id"    form:"user_id"`
}

// ChatResult 一次对话的文本结果。
//
// **不含密钥明文**：只有「用了哪个供应商 / 哪个模型 / 走了哪条协议」与拿回的文本。
type ChatResult struct {
	ProviderKey string `json:"provider_key"`
	Model       string `json:"model"`
	Output      string `json:"output"`
	Protocol    string `json:"protocol"`

	// 上游上报的用量（OpenAI 兼容协议的 usage 字段）。
	//
	// UsageReported=false 表示**这一家没有上报**，此时三列恒为 0 —— 语义是「不知道」而不是
	// 「用了 0 个 token」。调用流水里必须把这两种情况分开，否则未上报的调用会看起来像没消耗。
	// 刻意不做本地估算兜底：流水是事实记录，估算值混进去会被当成真用量。
	InputTokens   int64 `json:"input_tokens"`
	OutputTokens  int64 `json:"output_tokens"`
	TotalTokens   int64 `json:"total_tokens"`
	UsageReported bool  `json:"usage_reported"`
}

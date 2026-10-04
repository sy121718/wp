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
}

// ChatResult 一次对话的文本结果。
//
// **不含密钥明文**：只有「用了哪个供应商 / 哪个模型 / 走了哪条协议」与拿回的文本。
type ChatResult struct {
	ProviderKey string `json:"provider_key"`
	Model       string `json:"model"`
	Output      string `json:"output"`
	Protocol    string `json:"protocol"`
}

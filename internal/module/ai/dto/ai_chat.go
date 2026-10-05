package aidto

import "encoding/json"

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

	// Messages 完整的出站消息序列（优先于 Input）；为空时按 Input 构造一条 user 消息。
	//
	// 与 Input 并存是**过渡期的两个入口**：一问一答（provider 连通性探测、模型列表校验）
	// 只需要一句文本，而带工具的轮次需要「历史 + 工具往返」这种序列。
	// 让两者都走同一条出站路径，比给带工具的调用另开一个函数更省事 —— 后者会把
	// 协议分派、SSRF 校验、自定义头、密钥解密这一整串再复制一遍。
	//
	// **不接受调用方自报**（同 SessionID/UserID 的纪律）：出站消息由服务端拼，
	// 对外接口不提供注入任意消息的通道。
	Messages []ChatMessage `json:"-" form:"-"`
	// Tools 本次可用的工具声明；为空表示不带工具（服务端填，同上不接受自报）。
	Tools []ToolSpec `json:"-" form:"-"`
}

// ChatMessage 出站请求里的一条对话消息（OpenAI 兼容协议的最小交集）。
//
// 只描述「发出去的那一刻」，**不是持久化形状**：事件日志（ai_event）是通用真源
// （docs/16 §7 不做 provider 专有格式持久化），这里是出站时的一次性翻译结果。
type ChatMessage struct {
	// Role 取值 system / user / assistant / tool。
	// system 是常驻规则（ai/prompt 包），由会话层放在列表最前，与会话历史一起构成稳定前缀。
	Role    string `json:"role"`
	Content string `json:"content"`
	// ToolCalls 仅 assistant 消息使用：模型要求调用的工具。
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
	// ToolCallID 仅 tool 消息使用：这条结果对应哪一次调用。
	//
	// **必须回填**：模型侧靠它把结果与请求配对，缺了会被多数网关直接拒
	// （400，形如「tool_call_id 不匹配」）—— 而那看起来像协议版本不兼容，很难查。
	ToolCallID string `json:"toolCallId,omitempty"`
	// Name 工具名（仅 tool 消息；部分网关要求，缺了不报错但会降低结果的归属清晰度）。
	Name string `json:"name,omitempty"`
}

// ToolCall 一次工具调用请求。
type ToolCall struct {
	// ID 本次调用的标识，由上游生成（chat/completions 是 tool_calls[].id，
	// responses 是 function_call.call_id）；回灌结果时必须原样带回。
	ID string `json:"id"`
	// Name 工具名。
	Name string `json:"name"`
	// Arguments 调用的原始入参 JSON 文本。
	//
	// 保持 string 而不解析成结构：上游给的**可能不是合法 JSON**（截断、多段拼接），
	// 而「参数不合法」该由工具执行层按 schema 报一个可回给模型的错误，
	// 不该在解析响应时把整轮对话判成失败。
	Arguments string `json:"arguments"`
}

// ToolSpec 一个可调用工具的声明（发给上游的 tools 片段里与 provider 无关的那部分）。
//
// Parameters 是 JSON Schema 原文：调用方（internal/mcp）负责生成，本层只搬运 ——
// 协议层不该理解「什么参数算合法」，那是工具执行层的事。
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ChatResult 一次对话的文本结果。
//
// **不含密钥明文**：只有「用了哪个供应商 / 哪个模型 / 走了哪条协议」与拿回的文本。
type ChatResult struct {
	ProviderKey string `json:"provider_key"`
	Model       string `json:"model"`
	Output      string `json:"output"`
	Protocol    string `json:"protocol"`

	// ToolCalls 模型这一轮要求调用的工具。
	//
	// 非空时 Output 往往是空的 —— 那是**正常的中间态**（模型还没拿到结果，无话可说），
	// 不是「模型没回答」。调用方按「有 ToolCalls 就执行并再来一轮」处理，
	// 只有两者都空才算真的没产出。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

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

package aidto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

// ai_resp.go — ai 模块的响应形状（service → inbound）。
//
// 对外形状不借用 model 层实体（契约与存储解耦）：model 只回实体，service 负责翻译。

// ModelEntry 模型目录里的一行（config_data.models 的元素形状）。
//
// 接口形状用 camelCase（与其它后台 JSON 接口同口径），**落库形状是 snake_case**
// （id / display_name / context_window / max_output_tokens / input_types，见迁移 511
// 文件头）：两处字段名不同，翻译在 service 的 parseModels / modelsToConfig 一对函数里 ——
// 改这里的 JSON 名不会动落库名，反之亦然，别只改一边。
//
// ContextWindow / MaxOutputTokens 为 0 表示**未知**（拉取回来的目录只有 id，
// 元数据留空由管理员按需补），不是「0 token」的语义 —— 展示侧据此显示占位。
type ModelEntry struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"displayName"`
	ContextWindow   int64    `json:"contextWindow"`
	MaxOutputTokens int64    `json:"maxOutputTokens"`
	InputTypes      []string `json:"inputTypes"`
}

// Provider 一个供应商的完整视图（含模型目录）。
//
// **不含密钥明文**：只有 HasAPIKey（是否已配置）—— 密钥出库即加密，回读永远是布尔。
type Provider struct {
	ID          int64          `json:"id"`
	ProviderKey string         `json:"providerKey"`
	DisplayName string         `json:"displayName"`
	BaseURL     string         `json:"baseUrl"`
	Protocol    string         `json:"protocol"`
	Status      int            `json:"status"`
	Sort        int            `json:"sort"`
	Models      []ModelEntry   `json:"models"`
	HasAPIKey   bool           `json:"hasApiKey"`
	Version     int64          `json:"version"`
	UpdateTime  utils.JSONTime `json:"updateTime"`
}

// FetchModelsResult 「获取可用模型」的结果：落库后的供应商 + 本次新增的模型 ID。
//
// 返回 Added 而不是「只回供应商」：页面要告诉用户这次到底拉回了什么，
// 否则「获取可用模型」点了没反应（全已存在）与「拉了 0 个」看起来一样。
type FetchModelsResult struct {
	Provider *Provider `json:"provider"`
	Added    []string  `json:"added"`
	Fetched  int       `json:"fetched"`
}

// ModelCandidates 「获取可用模型」的候选集：**只拉取、不落库**，交用户在弹窗里勾选。
//
// 与 FetchModelsResult 的分工：那个是「拉完直接并进目录」（JSON 接口的历史行为），
// 这个是「把候选交出去」—— 页面侧只有在用户点「添加所选」之后才写库，
// 所以拉取本身不能有副作用（点开弹窗看一眼不该改动目录）。
//
// Candidates 是服务端返回的**原始 id 列表**（不排序、不加工、不剥前缀）；
// Existing 是目录里已有的 id，供弹窗标记「已在目录」——
// 有它前端才能把「已存在」与「这次新增」分开显示，而不是一律灰掉。
type ModelCandidates struct {
	Provider   *Provider `json:"provider"`
	Candidates []string  `json:"candidates"`
	Existing   []string  `json:"existing"`
	Fetched    int       `json:"fetched"`
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
	// Images 这条消息附带的图片（仅 user 消息使用）。
	//
	// 每个元素是**完整的 data URI**（`data:image/png;base64,...`），不是站内相对地址：
	// 上游服务在另一台机器上，`/uploads/xxx.png` 对它毫无意义 —— 发相对路径过去，
	// 表现是「模型说看不到图片」，而请求本身 200。
	//
	// 只在**本轮**的 user 消息上带图：历史里那一轮回填的是图片标识（文字），
	// 不是像素 —— 否则每问一句都要把前面所有图重发一遍，上下文与账单都跟着涨。
	Images []string `json:"images,omitempty"`
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
	// Reasoning 模型这一轮的思考过程（部分上游单独返回；没有就是空串）。
	//
	// **它不进对话历史**：那不是模型对用户说的话，回灌会给下一轮制造一段
	// 「模型说自己想过了什么」的伪上下文。它的用途只有展示。
	Reasoning string `json:"reasoning,omitempty"`
	Protocol  string `json:"protocol"`

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
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
	// CachedTokens 输入里命中上游前缀缓存的 token 数（docs/16 §3.1 命中率的唯一数据源）。
	//
	// 不另设 CachedReported：**这个结构体是出站层的返回值，不是落库行**——
	// 上游没报时它是 0，而下游要判「有没有报」看的是 UsageReported（usage 对象在不在）。
	// 分不清的只有一种情况：usage 在、但里面没有 details 子对象。那种情况下
	// 这一列记 0 而 UsageReported=true，计量侧会把它算成「未命中」——偏差方向是
	// 「命中率看起来更低」，不会让问题被掩盖。
	CachedTokens  int64 `json:"cached_tokens"`
	UsageReported bool  `json:"usage_reported"`
}

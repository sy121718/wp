// ai_msg_protocol.go — dsh 预设的协议字符串到本模块协议枚举的映射。
//
// 背景：dsh 侧 10 类预设用的协议字符串是**连字符**形态（openai-responses /
// google-generative-ai …），本模块的协议枚举是**下划线**形态（见 ai_msg.go 的
// Protocol* 常量与迁移 511 的 COMMENT）。两套字符串不通用，这里只做「外部字符串 →
// 内部枚举」的翻译，内部枚举值保持不变。
//
// 为什么不让内部枚举直接改用连字符串：511 迁移的 COMMENT 已写死下划线形态、DB 存量行
// 的 protocol 已按下划线取值、前端下拉与 IsSupportedProtocol 白名单都基于它。把内部
// 枚举耦合到外部字符串后患更大 —— 外部改一次拼写，内部存量数据跟着漂。
package aienums

// DSHProtocolAlias 把 dsh 预设里的连字符串映射到本模块的协议枚举值。
//
// 未登记的外部协议（bedrock-converse-stream / azure-openai-responses / google-vertex /
// mistral-conversations / openai-codex-responses / pi-messages）**故意不映射** ——
// 由调用方按「未知协议」处理，禁止静默回落成默认协议。
var DSHProtocolAlias = map[string]string{
	"openai-completions":   ProtocolOpenAIChatCompletions,
	"openai-responses":     ProtocolOpenAIResponses,
	"anthropic-messages":   ProtocolAnthropicMessages,
	"google-generative-ai": ProtocolGeminiGenerateContent,
}

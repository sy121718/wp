// ai_protocol_common.go — 两个 OpenAI 兼容协议共享的字面量、结果形状与助手。
//
// 放在这里而不是任一份协议实现里：两边都要用，谁先写谁拥有的结果会是
// 「改一个常量要先去另一份文件里找」。
package aiservice

import (
	"strings"

	aidto "go_wp/internal/module/ai/dto"
)

// 对话角色（两个协议共用同一套名字）。
//
// system 的来源是 ai/prompt 包的常驻规则（SiteRules），由会话层放在消息列表**最前**：
// 它与会话历史一起构成稳定前缀，同一会话里反复发消息时逐字节不变（docs/16 §3）。
// 曾经这里写着「规则拼进历史文本、不单独占 system 消息」，理由是怕规则变化作废缓存 ——
// 那条注释对应的实现从未存在（`buildChatMessages` 是个幽灵名字），而且理由本身是错位的：
// 规则改得**不频繁**，作废一次缓存可以接受；真正每轮都变的是历史与输入，它们本来就该在尾部。
const (
	roleSystem    = "system"
	roleUser      = "user"
	roleAssistant = "assistant"
	roleTool      = "tool"
)

// 工具相关的协议字面量（两家一致的部分）。
const (
	// toolTypeFunction 工具类型标记：chat/completions 在 tools[].type，responses 在
	// function_call 条目的 type。两家都用 "function"。
	toolTypeFunction = "function"
	// toolChoiceAuto 由模型自己决定这轮要不要调工具。
	//
	// 刻意不用 "required"（必须调一个）：那会让「今天有几单」这种本来就该直接回答的提问
	// 也被强行塞一次调用。也不设白名单 —— 工具集该由调用方裁剪（docs/17 D4），
	// 而不是在请求体里再写一遍同样的名单（两处名单必然分叉）。
	toolChoiceAuto = "auto"
)

// ProtocolReply 一次上游响应的解析结果（两个协议统一的出站形状）。
//
// Content 与 ToolCalls 的**组合语义**：只有 Content 是「回答完了」，
// 只有 ToolCalls 是「要做点事」，两者都有是「先说了句话、再要做事」，都空才是解析失败。
type ProtocolReply struct {
	// Content 模型说的话；要求调工具时通常为空。
	Content string
	// Reasoning 模型的思考过程（部分上游在正文之外单独返回，字段名两家不同：
	// reasoning_content / reasoning）。
	//
	// 它**不是模型说的话**，所以不进对话历史；它的用途只有一个 —— 展示给用户
	// 「它在想什么」。一次要跑工具的任务里正文可能几十秒不出字，那段时间用户
	// 只看到一个没反应的按钮，只能猜是不是卡死了。
	Reasoning string
	// ToolCalls 模型要求执行的工具调用（可能多个，按上游给的顺序执行）。
	ToolCalls []aidto.ToolCall
	// Usage 上游上报的用量（未上报时 Reported=false）。
	Usage ReplyUsage
}

// emptyObjectSchema 一个「无参数」的 JSON Schema。
//
// 不写 parameters 时补它：OpenAI 兼容网关对 tools[].function.parameters 为 null
// 的处理并不一致（有的当空对象、有的直接 400），补一个显式空对象能把差异抹平。
func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// orEmptyJSONObject 空 arguments 补成 "{}"。
//
// 回灌历史轮次时才需要：上游校验 arguments 是字符串，而空串不是合法 JSON 对象。
func orEmptyJSONObject(args string) string {
	if strings.TrimSpace(args) == "" {
		return "{}"
	}
	return args
}

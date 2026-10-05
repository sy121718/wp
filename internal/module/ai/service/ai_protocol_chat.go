// ai_protocol_chat.go — openai_chat_completions（默认协议）的请求体构造与响应解析。
//
// 与 responses 的镜像关系：请求体是 {model, messages:[...], tools?, max_tokens?}，正文取
// choices[0].message.content，工具调用取 choices[0].message.tool_calls。两者放在同一层，
// 由 ai_client.go 的分派函数二选一。
package aiservice

import (
	"encoding/json"
	"strings"

	aidto "go_wp/internal/module/ai/dto"
)

// buildChatCompletionsBody 构造 /chat/completions 的请求体。
//
// messages 的每个元素按角色带不同的附加字段：assistant 带 tool_calls、tool 带 tool_call_id。
// content **始终写入**（哪怕是空串）：带 tool_calls 的 assistant 消息正文本来就是空的，
// 而部分网关要求 content 字段存在 —— 省掉它换来的是「400 缺少 content」这种与语义无关的故障。
//
// tools 非空时才写 tool_choice：显式写 auto 让「这轮允许调工具」在请求体里可见，
// 排查「模型为什么不调工具」时不必再去翻服务端的默认值。
//
// max_tokens 只在 > 0 时写入：0 会被服务端当成「最多生成 0 个 token」直接截断。
func buildChatCompletionsBody(model string, msgs []aidto.ChatMessage, tools []aidto.ToolSpec, maxOutputTokens int64) ([]byte, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, ErrInvalidParam
	}
	if len(msgs) == 0 {
		return nil, ErrInvalidParam
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		item := map[string]any{"role": m.Role, "content": m.Content}
		if len(m.ToolCalls) > 0 {
			item["tool_calls"] = chatToolCalls(m.ToolCalls)
		}
		if m.ToolCallID != "" {
			item["tool_call_id"] = m.ToolCallID
		}
		if m.Name != "" {
			item["name"] = m.Name
		}
		out = append(out, item)
	}
	payload := map[string]any{"model": model, "messages": out}
	if len(tools) > 0 {
		payload["tools"] = chatTools(tools)
		payload["tool_choice"] = toolChoiceAuto
	}
	if maxOutputTokens > 0 {
		payload["max_tokens"] = maxOutputTokens
	}
	return json.Marshal(payload)
}

// chatTools 把工具声明翻成 chat/completions 的 tools 形状（每个工具外面包一层 type=function）。
//
// parameters 缺失时补一个空对象 schema：不补的话请求体里是 null，而 OpenAI 兼容网关对
// tools[].function.parameters 为 null 的处理并不一致（有的当空对象、有的直接 400）。
func chatTools(tools []aidto.ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		fn := map[string]any{"name": t.Name, "description": t.Description}
		if len(t.Parameters) > 0 {
			fn["parameters"] = json.RawMessage(t.Parameters)
		} else {
			fn["parameters"] = emptyObjectSchema()
		}
		out = append(out, map[string]any{"type": toolTypeFunction, "function": fn})
	}
	return out
}

// chatToolCalls 把工具调用翻回请求体的 tool_calls 形状（回灌历史轮次时用）。
func chatToolCalls(calls []aidto.ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, c := range calls {
		out = append(out, map[string]any{
			"id":   c.ID,
			"type": toolTypeFunction,
			"function": map[string]any{
				"name": c.Name,
				// arguments 缺失时补 "{}"：回灌时上游多半会校验它是字符串，
				// 而空串不是合法 JSON 对象，会让这一轮直接失败。
				"arguments": orEmptyJSONObject(c.Arguments),
			},
		})
	}
	return out
}

// parseChatCompletionsReply 从 /chat/completions 的响应体里取出正文、工具调用与用量。
//
// 形状不对（解不出 JSON、没有 choices）归口 ErrInternal；**正文为空但有待执行的工具调用不算错** ——
// 模型要调工具时正文本来就是空的，把这种中间态判成「服务器内部错误」会让第一次工具调用就失败。
// 判错的判据是「两者都空」：既没有话要说、也没有事要做，才是真的没产出。
//
// usage 缺失不算错：它只影响调用流水的用量列（Reported=false），正文该回还是要回 ——
// 把「这家没报 usage」判成失败会让一次成功的对话看起来像挂了。
func parseChatCompletionsReply(body []byte) (ProtocolReply, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return ProtocolReply{}, ErrInternal
	}
	usage := usageFromJSON(root["usage"])
	choices, _ := root["choices"].([]any)
	if len(choices) == 0 {
		return ProtocolReply{Usage: usage}, ErrInternal
	}
	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)
	text, _ := msg["content"].(string)
	calls := parseChatToolCalls(msg["tool_calls"])
	// 思考过程两家字段名不同：DeepSeek 系用 reasoning_content，另一些用 reasoning。
	// 都试一遍 —— 只认一个时，换一家供应商这个功能就静默消失（页面上看不出区别，
	// 只是「正在思考」那一段永远不出现）。
	reasoning := firstNonEmptyString(msg, "reasoning_content", "reasoning")
	// 空判断**不含 reasoning**：只有思考过程、没有正文也没有工具调用，等于这轮没产出。
	// 把它当成功会让外层拿一个空回答去写会话事件。
	if strings.TrimSpace(text) == "" && len(calls) == 0 {
		return ProtocolReply{Usage: usage}, ErrInternal
	}
	return ProtocolReply{Content: text, Reasoning: reasoning, ToolCalls: calls, Usage: usage}, nil
}

// firstNonEmptyString 按顺序取第一个非空的字符串字段。
func firstNonEmptyString(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := obj[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// parseChatToolCalls 解析 message.tool_calls。
//
// 没有工具名的条目**直接跳过**而不是造一条空调用：空名字的调用执行不了，
// 留在列表里只会让上层拿着一串「查不到这个工具」的错误去问模型，白烧一轮。
// 一个都没解出来时回 nil（与「本来就没有 tool_calls」等价）。
func parseChatToolCalls(raw any) []aidto.ToolCall {
	list, _ := raw.([]any)
	if len(list) == 0 {
		return nil
	}
	out := make([]aidto.ToolCall, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := obj["function"].(map[string]any)
		name, _ := fn["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		args, _ := fn["arguments"].(string)
		id, _ := obj["id"].(string)
		out = append(out, aidto.ToolCall{ID: id, Name: name, Arguments: args})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

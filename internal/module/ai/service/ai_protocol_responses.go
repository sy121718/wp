// ai_protocol_responses.go — openai_responses 协议的请求体构造与响应解析。
//
// 与 chat/completions 的差异（这也是 muse 系列只吃它的原因）：
//
//	· 请求体是 {model, input, max_output_tokens}，input 可以是字符串（单条消息）或消息数组；
//	· 工具往返**不是 message**：调用是 output[] 里 type=function_call 的独立条目，
//	  结果是 input[] 里 type=function_call_output 的独立条目（靠 call_id 配对）；
//	· 响应是 {"object":"response", ...}，正文藏在 output[].content[]（type=output_text）里，
//	  顶层没有 chat/completions 的 choices[0].message.content。
//
// 只做「构造 / 解析」这一层纯函数，不碰 DB、不发请求 —— 出站由 ai_client.go 的分派装配，
// 这样单测可以直接喂 JSON，不必起 HTTP 服务。
package aiservice

import (
	"encoding/json"
	"strings"

	aidto "go_wp/internal/module/ai/dto"
)

// responses 响应里各条目的类型标记。
const (
	responsesObjectName   = "response"
	responsesTextPartType = "output_text"
	// responsesCallPartType 工具调用条目（output[] 里独立一项）。
	responsesCallPartType = "function_call"
	// responsesCallOutputType 工具结果条目（input[] 里独立一项）。
	responsesCallOutputType = "function_call_output"
	// responsesMessagePartType 普通消息条目。
	responsesMessagePartType = "message"
	// responsesStatusFailed 是服务端明确宣告「这轮失败」，据此直接归错。
	// 状态 incomplete（被 max_output_tokens 截断 / 被内容过滤中止）语义不同：它不代表
	// 没有正文，所以这里**不建常量、也不参与判错**（见 parseResponsesReply 的取值路径），
	// 免得后人顺手拿它当失败状态用。
	responsesStatusFailed = "failed"
)

// buildResponsesBody 构造 /responses 的请求体。
//
// input 的形状按消息序列自适应：
//   - 只有一条平凡消息（无工具调用、无结果）时写成**字符串**，与历史实现逐字节一致 ——
//     这条路径覆盖了现在所有的调用（一问一答），字节不变就保住了上游的前缀缓存（docs/16 §3）；
//   - 出现工具往返时写成数组，让 function_call / function_call_output 能与 message 并列。
//
// max_output_tokens 只在 > 0 时写入 —— 服务端对缺省值有自己的默认，塞 0 会被当成
// 「最多生成 0 个 token」而立刻截断。
func buildResponsesBody(model string, msgs []aidto.ChatMessage, tools []aidto.ToolSpec, maxOutputTokens int64) ([]byte, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, ErrInvalidParam
	}
	if len(msgs) == 0 {
		return nil, ErrInvalidParam
	}
	payload := map[string]any{"model": model, "input": responsesInput(msgs)}
	if len(tools) > 0 {
		payload["tools"] = responsesTools(tools)
		payload["tool_choice"] = toolChoiceAuto
	}
	if maxOutputTokens > 0 {
		payload["max_output_tokens"] = maxOutputTokens
	}
	return json.Marshal(payload)
}

// responsesInput 把消息序列翻成 responses 的 input。
//
// 平凡序列（每个元素都是「有角色、无工具字段、正文非空」）回退成纯字符串形态：
// 字符串与「单条 user message」在服务端等价，但字节更短、且与历史实现完全一致。
func responsesInput(msgs []aidto.ChatMessage) any {
	if len(msgs) == 1 && isPlainMessage(msgs[0]) {
		return msgs[0].Content
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if len(m.ToolCalls) > 0 {
			// assistant 的正文与调用分开成两条：responses 的 function_call 条目没有 content 字段，
			// 硬塞进去会被服务端当未知字段忽略（用户看到的是「模型的话凭空消失」）。
			if text := strings.TrimSpace(m.Content); text != "" {
				out = append(out, map[string]any{"role": roleAssistant, "content": text})
			}
			for _, c := range m.ToolCalls {
				out = append(out, map[string]any{
					"type":      responsesCallPartType,
					"call_id":   c.ID,
					"name":      c.Name,
					"arguments": orEmptyJSONObject(c.Arguments),
				})
			}
			continue
		}
		if m.ToolCallID != "" {
			out = append(out, map[string]any{
				"type":    responsesCallOutputType,
				"call_id": m.ToolCallID,
				"output":  m.Content,
			})
			continue
		}
		out = append(out, map[string]any{"role": m.Role, "content": m.Content})
	}
	return out
}

// isPlainMessage 判断一条消息能否用字符串形态表达。
func isPlainMessage(m aidto.ChatMessage) bool {
	return m.Role == roleUser && m.ToolCallID == "" && len(m.ToolCalls) == 0 && m.Name == ""
}

// responsesTools 把工具声明翻成 responses 的 tools 形状（**扁平**，没有 chat/completions 的
// function 包装层 —— 少一层壳是多数字段名与参数位置都不同的常见坑）。
func responsesTools(tools []aidto.ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		item := map[string]any{"type": toolTypeFunction, "name": t.Name, "description": t.Description}
		if len(t.Parameters) > 0 {
			item["parameters"] = json.RawMessage(t.Parameters)
		} else {
			item["parameters"] = emptyObjectSchema()
		}
		out = append(out, item)
	}
	return out
}

// parseResponsesReply 从 /responses 的响应体里取出正文、工具调用与用量（usage）。
//
// usage 缺失不算错：它只影响调用流水的用量列（Reported=false），正文该回还是要回 ——
// 把「这家没报 usage」判成失败会让一次成功的对话看起来像挂了。
//
// 取值优先级：
//
//  1. 顶层 output_text（OpenAI SDK 的便捷聚合字段，部分网关会带上）；
//  2. 遍历 output[]，拼接所有 type=output_text 的 text 片段，并收集 type=function_call 的调用。
//
// object 不是 response、或状态是 failed 归口 ErrInternal（底层原文只进日志，不上页面）。
// 「正文为空**且**没有工具调用」才归错：模型要调工具时正文本来就是空的，
// 把这种中间态判成失败会让第一次工具调用就报错。
//
// status=incomplete（截断 / content_filter）**不提前返回**：它只说明这轮没跑到 completed，
// 正文可能照样生成，继续走下面的取值路径；真取不到片段时落到末尾唯一的「无正文」出口。
// 提前把 incomplete 归口 ErrInternal 会把「用户给的上限太小」伪装成「服务器内部错误」。
func parseResponsesReply(body []byte) (ProtocolReply, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return ProtocolReply{}, ErrInternal
	}
	if obj, _ := root["object"].(string); obj != "" && obj != responsesObjectName {
		return ProtocolReply{}, ErrInternal
	}
	if status, _ := root["status"].(string); status == responsesStatusFailed {
		return ProtocolReply{}, ErrInternal
	}
	// usage 缺失不算错（只影响调用流水的用量列），所以先取出来、后面每个出口都带上它。
	usage := usageFromJSON(root["usage"])

	output, _ := root["output"].([]any)
	calls := parseResponsesToolCalls(output)

	if text, ok := root["output_text"].(string); ok && text != "" {
		return ProtocolReply{Content: text, ToolCalls: calls, Usage: usage}, nil
	}

	var sb strings.Builder
	for _, item := range output {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := msg["type"].(string); kind != "" && kind != responsesMessagePartType {
			continue
		}
		content, _ := msg["content"].([]any)
		for _, part := range content {
			piece, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if kind, _ := piece["type"].(string); kind != responsesTextPartType {
				continue
			}
			if text, _ := piece["text"].(string); text != "" {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(text)
			}
		}
	}
	text := sb.String()
	if strings.TrimSpace(text) == "" && len(calls) == 0 {
		return ProtocolReply{Usage: usage}, ErrInternal
	}
	return ProtocolReply{Content: text, ToolCalls: calls, Usage: usage}, nil
}

// parseResponsesToolCalls 从 output[] 里收集 function_call 条目。
//
// 没有名字的条目跳过（执行不了，留着只会白烧一轮查询）；一个都没有时回 nil。
func parseResponsesToolCalls(output []any) []aidto.ToolCall {
	var out []aidto.ToolCall
	for _, item := range output {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := obj["type"].(string); kind != responsesCallPartType {
			continue
		}
		name, _ := obj["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		args, _ := obj["arguments"].(string)
		id, _ := obj["call_id"].(string)
		out = append(out, aidto.ToolCall{ID: id, Name: name, Arguments: args})
	}
	return out
}

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
	// responsesReasoningPartType 思考过程条目（output[] 里独立一项）。
	//
	// 在这一族协议里思维链不是 message 的一个字段，而是与 message 并列的一种条目：
	// output: [{type:"reasoning", summary:[{type:"summary_text", text:"..."}]}, {type:"message", ...}]。
	// 只读 content 会漏掉整段思考（而它恰恰是跑工具那几十秒里唯一的进展信号）。
	responsesReasoningPartType = "reasoning"
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
// **system 消息不走 input**：本协议把它放在顶层 `instructions`。这不是风格选择 ——
// input[] 里的合法角色是 user / assistant（工具条目另算），塞一个 system 进去会被服务端
// 400 拒掉。这个坑此前不会暴露，因为在这条改动之前**根本不存在 system 消息**
// （常驻规则那一段是刚加进会话层的）—— 也就是说，加了规则却不同时改这里，
// 第一次请求就会失败，而错误信息只会说请求体不合法。
//
// max_output_tokens 只在 > 0 时写入 —— 服务端对缺省值有自己的默认，塞 0 会被当成
// 「最多生成 0 个 token」而立刻截断。
func buildResponsesBody(model string, msgs []aidto.ChatMessage, tools []aidto.ToolSpec, maxOutputTokens int64, stream bool) ([]byte, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, ErrInvalidParam
	}
	if len(msgs) == 0 {
		return nil, ErrInvalidParam
	}
	instructions, rest := splitSystemMessages(msgs)
	// 抽走 system 之后 input 不能为空：上游要求 input 存在。
	// （正常路径下总会剩一条 user；真为空时按参数错误打回，而不是发一个必然被拒的请求体。）
	if len(rest) == 0 {
		return nil, ErrInvalidParam
	}
	payload := map[string]any{"model": model, "input": responsesInput(rest)}
	if stream {
		// 这一族的流式也走 SSE，但事件名自带语义（response.output_text.delta 等）；
		// 它**没有** chat 那种 stream_options.include_usage ——用量在
		// response.completed 事件的负载里，无需额外开关。
		payload["stream"] = true
	}
	if instructions != "" {
		payload["instructions"] = instructions
	}
	if len(tools) > 0 {
		payload["tools"] = responsesTools(tools)
		payload["tool_choice"] = toolChoiceAuto
	}
	if maxOutputTokens > 0 {
		payload["max_output_tokens"] = maxOutputTokens
	}
	return json.Marshal(payload)
}

// splitSystemMessages 把序列里的 system 消息抽出来（按原顺序、换行相连），其余原样保留。
//
// 多个 system 段用 `\n\n` 相连而不是覆盖：常驻规则与将来可能加的模块手册是两段独立文本，
// 覆盖会让后一段静默吃掉前一段（规则看着还在，实际只剩一半）。
// 顺序保持原样 —— 稳定前缀的字节稳定性依赖它（docs/16 §3）。
func splitSystemMessages(msgs []aidto.ChatMessage) (string, []aidto.ChatMessage) {
	var b strings.Builder
	rest := make([]aidto.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == roleSystem {
			// 刻意**不**做 TrimSpace：调用方给的就是要原样发出去的文本，
			// trim 会让「发出去的」与「调用方手里的」差一个尾换行 ——
			// 语义上没差，但前缀的字节稳定性就是由这种细节定义的（docs/16 §3）。
			if m.Content != "" {
				if b.Len() > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString(m.Content)
			}
			continue
		}
		rest = append(rest, m)
	}
	return b.String(), rest
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
		out = append(out, map[string]any{"role": m.Role, "content": responsesMessageContent(m)})
	}
	return out
}

// responsesMessageContent 与 chatMessageContent 同职，但分片形状不同：
// 这一族的图片是 {"type":"input_image","image_url":"<url 字符串>"}（**不是对象**），
// 文本是 {"type":"input_text","text":...}。把 url 写成对象会被服务端按未知结构忽略，
// 表现同样是「模型看不到图片」而请求 200。
func responsesMessageContent(m aidto.ChatMessage) any {
	if len(m.Images) == 0 {
		return m.Content
	}
	parts := make([]map[string]any, 0, len(m.Images)+1)
	if text := strings.TrimSpace(m.Content); text != "" {
		parts = append(parts, map[string]any{"type": "input_text", "text": m.Content})
	}
	for _, url := range m.Images {
		parts = append(parts, map[string]any{"type": "input_image", "image_url": url})
	}
	return parts
}

// isPlainMessage 判断一条消息能否用字符串形态表达。
func isPlainMessage(m aidto.ChatMessage) bool {
	// 带图的消息不能退成裸字符串：那样图片会被静默丢掉（上游收到一个字符串，
	// 它不知道里面该有图），而请求本身是 200。
	return m.Role == roleUser && m.ToolCallID == "" && len(m.ToolCalls) == 0 && m.Name == "" && len(m.Images) == 0
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
// parseResponsesReasoning 收集 output[] 里所有 reasoning 条目的文本。
//
// 两种承载都读：summary（这一族协议的常见形态）与 content（部分网关直接给 content）。
// 多段用换行相连 —— 模型一次可能给出多段思考，丢掉后面的会让用户只看到开头。
func parseResponsesReasoning(output []any) string {
	var parts []string
	for _, item := range output {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := obj["type"].(string); kind != responsesReasoningPartType {
			continue
		}
		for _, key := range []string{"summary", "content"} {
			for _, seg := range asAnyList(obj[key]) {
				if m, ok := seg.(map[string]any); ok {
					parts = append(parts, firstNonEmptyString(m, "text", "summary_text"))
				} else if s, ok := seg.(string); ok && strings.TrimSpace(s) != "" {
					parts = append(parts, s)
				}
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// asAnyList 把可能是数组的字段摊成 []any（不是数组时回 nil）。
func asAnyList(v any) []any {
	if list, ok := v.([]any); ok {
		return list
	}
	return nil
}

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
	reasoning := parseResponsesReasoning(output)

	if text, ok := root["output_text"].(string); ok && text != "" {
		return ProtocolReply{Content: text, Reasoning: reasoning, ToolCalls: calls, Usage: usage}, nil
	}

	text := parseResponsesOutputText(output)
	if strings.TrimSpace(text) == "" && len(calls) == 0 {
		return ProtocolReply{Usage: usage}, ErrInternal
	}
	return ProtocolReply{Content: text, Reasoning: reasoning, ToolCalls: calls, Usage: usage}, nil
}

// parseResponsesOutputText 从 output[] 里抽出助手说给用户的话。
//
// 抽出来单独成函数，是因为它有**两个**消费方：非流式的 parseResponsesReply，
// 以及流式的 response.completed 兜底。
//
// 流式那条为什么要兜底：这一族协议在流式下**并非一定**发
// `response.output_text.delta` 事件 —— 实测遇到过一次「只有 completed 事件、
// 里面 output[] 带着完整正文」，而那时只认 delta 的累积器是空的，
// 于是模型明明答了、系统却报「没能拿到回答」。
// 判据：completed 到达时若累积器是空的，就用这一份填上。
func parseResponsesOutputText(output []any) string {
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
	return sb.String()
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

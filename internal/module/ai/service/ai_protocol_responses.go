// ai_protocol_responses.go — openai_responses 协议的请求体构造与响应解析。
//
// 与 chat/completions 的差异（这也是 muse 系列只吃它的原因）：
//
//	· 请求体是 {model, input, max_output_tokens}，input 直接是字符串（不是 messages 数组）；
//	· 响应是 {"object":"response", ...}，正文藏在 output[].content[]（type=output_text）里，
//	  顶层没有 chat/completions 的 choices[0].message.content。
//
// 只做「构造 / 解析」这一层纯函数，不碰 DB、不发请求 —— 出站由 ai_client.go 的分派装配，
// 这样单测可以直接喂 JSON，不必起 HTTP 服务。
package aiservice

import (
	"encoding/json"
	"strings"
)

// responses 响应里正文字段的类型标记。
const (
	responsesObjectName   = "response"
	responsesTextPartType = "output_text"
	// responsesStatusFailed 是服务端明确宣告「这轮失败」，据此直接归错。
	// 状态 incomplete（被 max_output_tokens 截断 / 被内容过滤中止）语义不同：它不代表
	// 没有正文，所以这里**不建常量、也不参与判错**（见 parseResponsesReply 的取值路径），
	// 免得后人顺手拿它当失败状态用。
	responsesStatusFailed = "failed"
)

// buildResponsesBody 构造 /responses 的请求体：{model, input, max_output_tokens}。
//
// max_output_tokens 只在 > 0 时写入 —— 服务端对缺省值有自己的默认，塞 0 会被当成
// 「最多生成 0 个 token」而立刻截断。
func buildResponsesBody(model, input string, maxOutputTokens int64) ([]byte, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, ErrInvalidParam
	}
	payload := map[string]any{
		"model": model,
		"input": input,
	}
	if maxOutputTokens > 0 {
		payload["max_output_tokens"] = maxOutputTokens
	}
	return json.Marshal(payload)
}

// parseResponsesReply 从 /responses 的响应体里取出回复正文与用量（usage）。
//
// usage 缺失不算错：它只影响调用流水的用量列（Reported=false），正文该回还是要回 ——
// 把「这家没报 usage」判成失败会让一次成功的对话看起来像挂了。
//
// 取值优先级：
//
//  1. 顶层 output_text（OpenAI SDK 的便捷聚合字段，部分网关会带上）；
//  2. 遍历 output[].content[]，拼接所有 type=output_text 的 text 片段。
//
// object 不是 response、或状态是 failed、或一个文本片段都没有，都归口 ErrInternal
// （底层原文只进日志，不上页面）。
//
// status=incomplete（截断 / content_filter）**不提前返回**：它只说明这轮没跑到 completed，
// 正文可能照样生成，继续走下面的取值路径；真取不到片段时落到末尾唯一的「无正文」出口。
// 提前把 incomplete 归口 ErrInternal 会把「用户给的上限太小」伪装成「服务器内部错误」。
func parseResponsesReply(body []byte) (string, ReplyUsage, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return "", ReplyUsage{}, ErrInternal
	}
	if obj, _ := root["object"].(string); obj != "" && obj != responsesObjectName {
		return "", ReplyUsage{}, ErrInternal
	}
	if status, _ := root["status"].(string); status == responsesStatusFailed {
		return "", ReplyUsage{}, ErrInternal
	}
	// usage 缺失不算错（只影响调用流水的用量列），所以先取出来、后面每个出口都带上它。
	usage := usageFromJSON(root["usage"])

	if text, ok := root["output_text"].(string); ok && text != "" {
		return text, usage, nil
	}

	var sb strings.Builder
	output, _ := root["output"].([]any)
	for _, item := range output {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := msg["type"].(string); kind != "" && kind != "message" {
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
	if sb.Len() == 0 {
		return "", usage, ErrInternal
	}
	return sb.String(), usage, nil
}

// ai_protocol_chat.go — openai_chat_completions（默认协议）的请求体构造与响应解析。
//
// 与 responses 的镜像关系：请求体是 {model, messages:[{role,content}]}，正文取
// choices[0].message.content。两者放在同一层，由 ai_client.go 的分派函数二选一。
package aiservice

import (
	"encoding/json"
	"strings"
)

// chatRoleUser 对话请求里唯一的角色 —— 本层只做「一问」的最小请求，多轮由会话层拼装。
const chatRoleUser = "user"

// buildChatCompletionsBody 构造 /chat/completions 的请求体：{model, messages, max_tokens}。
//
// max_tokens 只在 > 0 时写入：0 会被服务端当成「最多生成 0 个 token」直接截断。
func buildChatCompletionsBody(model, input string, maxOutputTokens int64) ([]byte, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, ErrInvalidParam
	}
	payload := map[string]any{
		"model":    model,
		"messages": []map[string]any{{"role": chatRoleUser, "content": input}},
	}
	if maxOutputTokens > 0 {
		payload["max_tokens"] = maxOutputTokens
	}
	return json.Marshal(payload)
}

// parseChatCompletionsReply 从 /chat/completions 的响应体里取出回复正文
// （choices[0].message.content）。形状不对、或正文为空，归口 ErrInternal。
func parseChatCompletionsReply(body []byte) (string, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return "", ErrInternal
	}
	choices, _ := root["choices"].([]any)
	if len(choices) == 0 {
		return "", ErrInternal
	}
	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)
	text, _ := msg["content"].(string)
	if text == "" {
		return "", ErrInternal
	}
	return text, nil
}

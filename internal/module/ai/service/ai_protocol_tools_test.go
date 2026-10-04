package aiservice

// ai_protocol_tools_test.go — 工具往返的协议层用例。
//
// 这一层的判据是「两家的字段位置不同、但语义必须一致」：
// chat/completions 把工具包在 tools[].function 里、调用塞在 message.tool_calls 里；
// responses 的 tools 是扁平的、调用是 output[] 里的独立条目。
// 写错位置不会报错，只会表现为「模型看得见工具却从不调用」—— 所以逐字段钉住。

import (
	"encoding/json"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
)

func toolSpec(name string) aidto.ToolSpec {
	return aidto.ToolSpec{
		Name:        name,
		Description: "desc-" + name,
		Parameters: json.RawMessage(
			`{"type":"object","properties":{"projectId":{"type":"string"}},"required":["projectId"],"additionalProperties":false}`),
	}
}

// toolRoundMessages 一轮完整的工具往返：用户提问 → 模型要求调用 → 工具结果。
func toolRoundMessages() []aidto.ChatMessage {
	return []aidto.ChatMessage{
		{Role: roleUser, Content: "有多少单"},
		{Role: roleAssistant, ToolCalls: []aidto.ToolCall{
			{ID: "call_1", Name: "orders_summary", Arguments: `{"projectId":"p1"}`},
		}},
		{Role: roleTool, ToolCallID: "call_1", Name: "orders_summary", Content: "120 单"},
	}
}

// at 取解码后对象的第 i 项（类型断言失败即 Fatal，避免用 .(map[string]any) 一路 panic）。
func at(t *testing.T, list any, i int) map[string]any {
	t.Helper()
	items, ok := list.([]any)
	if !ok {
		t.Fatalf("不是数组：%T", list)
	}
	if i >= len(items) {
		t.Fatalf("数组只有 %d 项，取不到第 %d 项", len(items), i)
	}
	obj, ok := items[i].(map[string]any)
	if !ok {
		t.Fatalf("第 %d 项不是对象：%T", i, items[i])
	}
	return obj
}

// === chat/completions ===

func TestBuildChatCompletionsBody_WithTools(t *testing.T) {
	body, err := buildChatCompletionsBody("m", userMsgs("有多少单"), []aidto.ToolSpec{toolSpec("orders_summary")}, 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	m := decodeBody(t, body)
	if m["tool_choice"] != toolChoiceAuto {
		t.Errorf("tool_choice = %v，期望 %q", m["tool_choice"], toolChoiceAuto)
	}
	first := at(t, m["tools"], 0)
	if first["type"] != toolTypeFunction {
		t.Errorf("tools[0].type = %v", first["type"])
	}
	fn, _ := first["function"].(map[string]any)
	if fn["name"] != "orders_summary" || fn["description"] != "desc-orders_summary" {
		t.Errorf("tools[0].function = %v", fn)
	}
	params, ok := fn["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters 应是 JSON 对象，实得 %T", fn["parameters"])
	}
	if params["additionalProperties"] != false {
		t.Errorf("parameters.additionalProperties = %v，应保留 schema 原样", params["additionalProperties"])
	}
}

// TestBuildChatCompletionsBody_WithoutToolsOmitsToolFields 不带工具时不能出现 tools / tool_choice。
//
// 多写这两个字段会让本来「只是问一句话」的调用也被模型当成可能调工具的轮次，
// 而且给稳定前缀平白加了一段会随工具集变化的字节（docs/16 §3）。
func TestBuildChatCompletionsBody_WithoutToolsOmitsToolFields(t *testing.T) {
	body, err := buildChatCompletionsBody("m", userMsgs("你好"), nil, 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	m := decodeBody(t, body)
	if _, ok := m["tools"]; ok {
		t.Error("无工具时不应写 tools")
	}
	if _, ok := m["tool_choice"]; ok {
		t.Error("无工具时不应写 tool_choice")
	}
}

func TestBuildChatCompletionsBody_ToolRoundTrip(t *testing.T) {
	body, err := buildChatCompletionsBody("m", toolRoundMessages(), []aidto.ToolSpec{toolSpec("orders_summary")}, 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	m := decodeBody(t, body)

	second := at(t, m["messages"], 1)
	if second["role"] != roleAssistant {
		t.Fatalf("messages[1].role = %v", second["role"])
	}
	// content 字段必须存在（哪怕空串）：部分网关要求它，省掉换来的是与语义无关的 400。
	if _, ok := second["content"]; !ok {
		t.Error("带 tool_calls 的 assistant 消息仍要写 content 字段")
	}
	call := at(t, second["tool_calls"], 0)
	if call["id"] != "call_1" {
		t.Errorf("tool_calls[0].id = %v", call["id"])
	}
	callFn, _ := call["function"].(map[string]any)
	if callFn["name"] != "orders_summary" || callFn["arguments"] != `{"projectId":"p1"}` {
		t.Errorf("tool_calls[0].function = %v", callFn)
	}

	third := at(t, m["messages"], 2)
	if third["tool_call_id"] != "call_1" {
		t.Errorf("tool 消息必须回填 tool_call_id，实得 %v", third["tool_call_id"])
	}
	if third["content"] != "120 单" {
		t.Errorf("tool 消息 content = %v", third["content"])
	}
}

// TestBuildChatCompletionsBody_EmptyArgumentsBecomeObject 回灌时空 arguments 补成 "{}"：
// 空串不是合法 JSON 对象，会被上游拒掉整轮。
func TestBuildChatCompletionsBody_EmptyArgumentsBecomeObject(t *testing.T) {
	msgs := []aidto.ChatMessage{
		{Role: roleUser, Content: "x"},
		{Role: roleAssistant, ToolCalls: []aidto.ToolCall{{ID: "c1", Name: "noop"}}},
	}
	body, err := buildChatCompletionsBody("m", msgs, nil, 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	call := at(t, at(t, decodeBody(t, body)["messages"], 1)["tool_calls"], 0)
	fn, _ := call["function"].(map[string]any)
	if fn["arguments"] != "{}" {
		t.Errorf("arguments = %v，期望 {}", fn["arguments"])
	}
}

func TestParseChatCompletionsReply_ToolCallOnlyIsSuccess(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function",` +
		`"function":{"name":"orders_summary","arguments":"{\"projectId\":\"p1\"}"}}]}}],` +
		`"usage":{"prompt_tokens":9,"completion_tokens":3}}`)
	rep, err := parseChatCompletionsReply(body)
	if err != nil {
		t.Fatalf("正文为空但有工具调用不该报错: %v", err)
	}
	if len(rep.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v", rep.ToolCalls)
	}
	if rep.ToolCalls[0].Name != "orders_summary" || rep.ToolCalls[0].Arguments != `{"projectId":"p1"}` {
		t.Errorf("ToolCalls[0] = %+v", rep.ToolCalls[0])
	}
	if rep.Content != "" {
		t.Errorf("Content = %q，这一轮模型没说话", rep.Content)
	}
	if !rep.Usage.Reported {
		t.Error("usage 应被读出来（工具调用轮次一样烧 token）")
	}
}

// TestParseChatCompletionsReply_NamelessToolCallDoesNotRescueEmptyReply 没名字的调用跳过之后，
// 这轮就真的是「既没说话也没做事」—— 仍要报错，否则上层会拿着空调用列表空转一圈。
func TestParseChatCompletionsReply_NamelessToolCallDoesNotRescueEmptyReply(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"c1","function":{"arguments":"{}"}}]}}]}`)
	if _, err := parseChatCompletionsReply(body); err == nil {
		t.Fatal("没有正文、也没有可执行的调用，应报错")
	}
}

// === responses ===

func TestBuildResponsesBody_ToolRoundTrip(t *testing.T) {
	body, err := buildResponsesBody("m", toolRoundMessages(), []aidto.ToolSpec{toolSpec("orders_summary")}, 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	m := decodeBody(t, body)

	if _, ok := m["input"].([]any); !ok {
		t.Fatalf("带工具往返时 input 应是数组，实得 %T", m["input"])
	}
	call := at(t, m["input"], 1)
	if call["type"] != responsesCallPartType || call["call_id"] != "call_1" || call["name"] != "orders_summary" {
		t.Errorf("input[1] = %v", call)
	}
	if call["arguments"] != `{"projectId":"p1"}` {
		t.Errorf("input[1].arguments = %v", call["arguments"])
	}
	out := at(t, m["input"], 2)
	if out["type"] != responsesCallOutputType || out["call_id"] != "call_1" || out["output"] != "120 单" {
		t.Errorf("input[2] = %v", out)
	}

	first := at(t, m["tools"], 0)
	if first["type"] != toolTypeFunction || first["name"] != "orders_summary" {
		t.Errorf("tools[0] = %v", first)
	}
	// responses 的 tools 是扁平的：多包一层 function 会让服务端整段忽略这些工具。
	if _, ok := first["function"]; ok {
		t.Error("responses 的 tools 不应有 function 包装层")
	}
}

// TestBuildResponsesBody_KeepsAssistantTextWithCalls assistant 同时有正文与调用时，
// 正文必须单独成一条 message：function_call 条目没有 content 字段，硬塞会被当未知字段丢掉。
func TestBuildResponsesBody_KeepsAssistantTextWithCalls(t *testing.T) {
	msgs := []aidto.ChatMessage{
		{Role: roleUser, Content: "x"},
		{Role: roleAssistant, Content: "我先查一下", ToolCalls: []aidto.ToolCall{{ID: "c1", Name: "orders_summary", Arguments: "{}"}}},
	}
	body, err := buildResponsesBody("m", msgs, nil, 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	input := decodeBody(t, body)["input"]
	text := at(t, input, 1)
	if text["role"] != roleAssistant || text["content"] != "我先查一下" {
		t.Errorf("input[1] = %v，模型的话不该丢", text)
	}
	if at(t, input, 2)["type"] != responsesCallPartType {
		t.Error("正文之后应紧跟 function_call 条目")
	}
}

func TestParseResponsesReply_FunctionCallOnlyIsSuccess(t *testing.T) {
	body := []byte(`{"object":"response","status":"completed","output":[{"type":"function_call",` +
		`"call_id":"call_1","name":"orders_summary","arguments":"{\"projectId\":\"p1\"}"}],` +
		`"usage":{"input_tokens":9,"output_tokens":3}}`)
	rep, err := parseResponsesReply(body)
	if err != nil {
		t.Fatalf("正文为空但有工具调用不该报错: %v", err)
	}
	if len(rep.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v", rep.ToolCalls)
	}
	if rep.ToolCalls[0].ID != "call_1" || rep.ToolCalls[0].Name != "orders_summary" {
		t.Errorf("ToolCalls[0] = %+v", rep.ToolCalls[0])
	}
	if !rep.Usage.Reported {
		t.Error("usage 应被读出来")
	}
}

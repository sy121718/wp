package aiservice

// ai_protocol_responses_test.go — 协议层纯逻辑单测：请求体字段、响应解析、分派、
// 以及 config_data.headers 的自定义头注入（含 {{uuid}}）。
//
// 这些用例**不碰数据库、不起 HTTP 服务**：构造 / 解析是纯函数，出站装配用包级
// validateURL / newHeaderUUID 两个注入点把外部依赖摘掉。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
)

// userMsgs 构造「一条 user 消息」的入参：协议层用例里绝大多数都是它。
func userMsgs(text string) []aidto.ChatMessage {
	return []aidto.ChatMessage{{Role: roleUser, Content: text}}
}

func decodeBody(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("响应体不是 JSON: %v", err)
	}
	return m
}

// === responses 请求体 ===

func TestBuildResponsesBody_Fields(t *testing.T) {
	body, err := buildResponsesBody("muse-spark-1.3-contributor", userMsgs("ping"), nil, 16)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	m := decodeBody(t, body)
	if m["model"] != "muse-spark-1.3-contributor" {
		t.Errorf("model = %v", m["model"])
	}
	if m["input"] != "ping" {
		t.Errorf("input = %v", m["input"])
	}
	if got, _ := m["max_output_tokens"].(float64); got != 16 {
		t.Errorf("max_output_tokens = %v", m["max_output_tokens"])
	}
	if _, ok := m["messages"]; ok {
		t.Error("responses 请求体不应带 messages 字段")
	}
}

func TestBuildResponsesBody_OmitsMaxTokensWhenNonPositive(t *testing.T) {
	body, err := buildResponsesBody("m", userMsgs("x"), nil, 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if _, ok := decodeBody(t, body)["max_output_tokens"]; ok {
		t.Error("max_output_tokens<=0 时不应写入该字段")
	}
}

func TestBuildResponsesBody_RejectsEmptyModel(t *testing.T) {
	if _, err := buildResponsesBody("   ", userMsgs("x"), nil, 1); err == nil {
		t.Fatal("空 model 应报错")
	}
}

// === responses 响应解析 ===

func TestParseResponsesReply_FromOutputContent(t *testing.T) {
	body := []byte(`{"object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"pong"}]}]}`)
	rep, err := parseResponsesReply(body)
	got := rep.Content
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "pong" {
		t.Errorf("got = %q", got)
	}
}

func TestParseResponsesReply_PrefersTopLevelOutputText(t *testing.T) {
	body := []byte(`{"object":"response","status":"completed","output_text":"direct","output":[{"type":"message","content":[{"type":"output_text","text":"nested"}]}]}`)
	rep, err := parseResponsesReply(body)
	got := rep.Content
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "direct" {
		t.Errorf("got = %q", got)
	}
}

func TestParseResponsesReply_ConcatenatesParts(t *testing.T) {
	body := []byte(`{"object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"a"},{"type":"output_text","text":"b"}]}]}`)
	rep, err := parseResponsesReply(body)
	got := rep.Content
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "a\nb" {
		t.Errorf("got = %q", got)
	}
}

func TestParseResponsesReply_RejectsFailedStatus(t *testing.T) {
	body := []byte(`{"object":"response","status":"failed","output":[]}`)
	if _, err := parseResponsesReply(body); err == nil {
		t.Fatal("status=failed 应报错")
	}
}

// TestParseResponsesReply_FailedStatusStillRejectedWithText 真失败不因「碰巧有正文」被放行。
func TestParseResponsesReply_FailedStatusStillRejectedWithText(t *testing.T) {
	body := []byte(`{"object":"response","status":"failed","output_text":"partial","output":[{"type":"message","content":[{"type":"output_text","text":"partial"}]}]}`)
	if _, err := parseResponsesReply(body); err == nil {
		t.Fatal("status=failed 即使带正文也应报错")
	}
}

// TestParseResponsesReply_IncompleteWithTextReturnsText 截断（max_output_tokens / content_filter）
// 不是失败：正文已生成就要取回来 —— 把「用户给的上限太小」归口「服务器内部错误」是主 bug。
func TestParseResponsesReply_IncompleteWithTextReturnsText(t *testing.T) {
	body := []byte(`{"object":"response","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","content":[{"type":"output_text","text":"half an answer"}]}]}`)
	rep, err := parseResponsesReply(body)
	got := rep.Content
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "half an answer" {
		t.Errorf("got = %q", got)
	}
}

// TestParseResponsesReply_IncompleteWithoutTextRejected incomplete 且确实没有正文 → 仍归口错误。
func TestParseResponsesReply_IncompleteWithoutTextRejected(t *testing.T) {
	body := []byte(`{"object":"response","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}`)
	if _, err := parseResponsesReply(body); err == nil {
		t.Fatal("status=incomplete 且无正文片段应报错")
	}
}

func TestParseResponsesReply_RejectsWrongObject(t *testing.T) {
	body := []byte(`{"object":"chat.completion","output_text":"x"}`)
	if _, err := parseResponsesReply(body); err == nil {
		t.Fatal("object 不是 response 应报错")
	}
}

func TestParseResponsesReply_RejectsNoText(t *testing.T) {
	body := []byte(`{"object":"response","status":"completed","output":[]}`)
	if _, err := parseResponsesReply(body); err == nil {
		t.Fatal("没有任何文本片段应报错")
	}
}

// === 用量（usage）解析 ===
//
// 这一段的判据是「没上报」与「真的是 0」必须分得开：调用流水（ai_call_log）里
// 未上报的调用如果记成 0，看起来就像没消耗，统计会被静默带偏。

func TestParseResponsesReply_ReadsUsage(t *testing.T) {
	body := []byte(`{"object":"response","status":"completed","output_text":"pong","usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`)
	rep, err := parseResponsesReply(body)
	usage := rep.Usage
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !usage.Reported || usage.InputTokens != 11 || usage.OutputTokens != 7 || usage.TotalTokens != 18 {
		t.Fatalf("usage = %+v", usage)
	}
}

// TestParseResponsesReply_UsageMissingIsNotReported 没有 usage 对象 → Reported=false（不是 0 消耗）。
func TestParseResponsesReply_UsageMissingIsNotReported(t *testing.T) {
	body := []byte(`{"object":"response","status":"completed","output_text":"pong"}`)
	rep, err := parseResponsesReply(body)
	usage := rep.Usage
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if usage.Reported {
		t.Fatalf("没有 usage 对象时不该报 Reported：%+v", usage)
	}
}

// TestParseResponsesReply_UsageTotalFilledFromParts 上游只报前两个数时 total 由本地补齐。
func TestParseResponsesReply_UsageTotalFilledFromParts(t *testing.T) {
	body := []byte(`{"object":"response","status":"completed","output_text":"pong","usage":{"input_tokens":3,"output_tokens":4}}`)
	rep, err := parseResponsesReply(body)
	usage := rep.Usage
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if usage.TotalTokens != 7 {
		t.Fatalf("total 应补齐成 7，实际 %d（%+v）", usage.TotalTokens, usage)
	}
}

// TestParseChatCompletionsReply_ReadsUsage chat/completions 的字段名是 prompt/completion_tokens。
func TestParseChatCompletionsReply_ReadsUsage(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"pong"}}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`)
	rep, err := parseChatCompletionsReply(body)
	text := rep.Content
	usage := rep.Usage
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if text != "pong" {
		t.Fatalf("text = %q", text)
	}
	if !usage.Reported || usage.InputTokens != 5 || usage.OutputTokens != 2 || usage.TotalTokens != 7 {
		t.Fatalf("usage = %+v", usage)
	}
}

// TestParseChatCompletionsReply_KeepsUsageOnParseFailure 解析失败也要把已读到的 usage 带回去 ——
// 上游返回了正文之外的坏形状时，用量仍然是有价值的观测数据（调用流水照记）。
func TestParseChatCompletionsReply_KeepsUsageOnParseFailure(t *testing.T) {
	body := []byte(`{"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":1}}`)
	rep, err := parseChatCompletionsReply(body)
	usage := rep.Usage
	if err == nil || !usage.Reported || usage.TotalTokens != 10 {
		t.Fatalf("err = %v, usage = %+v", err, usage)
	}
}

// === 协议分派（禁止静默回落） ===

func TestProtocolPath_Dispatch(t *testing.T) {
	cases := map[string]string{
		aienums.ProtocolOpenAIChatCompletions: "/chat/completions",
		aienums.ProtocolOpenAIResponses:       "/responses",
	}
	for proto, want := range cases {
		got, err := protocolPath(proto)
		if err != nil || got != want {
			t.Errorf("%s → (%q, %v)", proto, got, err)
		}
	}
}

func TestProtocolPath_RejectsUnimplemented(t *testing.T) {
	if _, err := protocolPath(aienums.ProtocolAnthropicMessages); err == nil {
		t.Fatal("未实现的协议必须报错，禁止静默回落成 chat/completions")
	}
}

func TestBuildProtocolBody_RejectsUnimplemented(t *testing.T) {
	if _, err := buildProtocolBody(aienums.ProtocolGeminiGenerateContent, "m", userMsgs("x"), nil, 1); err == nil {
		t.Fatal("未实现的协议必须报错")
	}
}

func TestBuildProtocolRequest_UnknownProtocolDoesNotFallBack(t *testing.T) {
	origValidate := validateURL
	validateURL = func(string) error { return nil }
	defer func() { validateURL = origValidate }()

	svc := &Service{}
	provider := &aimodel.AIProviderEntity{
		ProviderKey: "opencode-go",
		BaseURL:     "https://opencode.ai/zen/go/v1",
		Protocol:    aienums.ProtocolAnthropicMessages,
	}
	if _, err := svc.buildProtocolRequest(context.Background(), provider, protocolRequestInput{Model: "m", Messages: userMsgs("x")}); err == nil {
		t.Fatal("未实现协议不应被静默回落")
	}
}

// === 自定义头与 {{uuid}} ===

func TestApplyProviderHeaders_UUIDSharedWithinRequest(t *testing.T) {
	orig := newHeaderUUID
	newHeaderUUID = func() string { return "uuid-fixed" }
	defer func() { newHeaderUUID = orig }()

	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	applyProviderHeaders(req, map[string]string{
		"x-opencode-session": "{{uuid}}",
		"x-trace":            "trace-{{uuid}}",
	})
	if got := req.Header.Get("x-opencode-session"); got != "uuid-fixed" {
		t.Errorf("session = %q", got)
	}
	if got := req.Header.Get("x-trace"); got != "trace-uuid-fixed" {
		t.Errorf("trace = %q", got)
	}
}

func TestApplyProviderHeaders_NewUUIDPerCall(t *testing.T) {
	orig := newHeaderUUID
	defer func() { newHeaderUUID = orig }()
	seq := 0
	newHeaderUUID = func() string { seq++; return fmt.Sprintf("uuid-%d", seq) }

	r1, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	r2, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	applyProviderHeaders(r1, map[string]string{"x-opencode-session": "{{uuid}}"})
	applyProviderHeaders(r2, map[string]string{"x-opencode-session": "{{uuid}}"})
	if r1.Header.Get("x-opencode-session") == r2.Header.Get("x-opencode-session") {
		t.Error("两次请求的 UUID 应各自新生成")
	}
}

func TestProviderHeaders_ReadsStringValuesOnly(t *testing.T) {
	cfg := aimodel.JSONMap{"headers": map[string]any{
		"x-opencode-session": "{{uuid}}",
		"x-empty":            "",
		"x-num":              123,
	}}
	got := providerHeaders(cfg)
	if got["x-opencode-session"] != "{{uuid}}" {
		t.Errorf("headers = %v", got)
	}
	if _, ok := got["x-num"]; ok {
		t.Error("非字符串值应跳过")
	}
	if _, ok := got["x-empty"]; !ok {
		t.Error("空串值仍应保留该键")
	}
}

func TestProviderHeaders_NilWhenAbsent(t *testing.T) {
	if got := providerHeaders(aimodel.JSONMap{"models": []any{}}); got != nil {
		t.Errorf("无 headers 键应回 nil，got = %v", got)
	}
	if got := providerHeaders(nil); got != nil {
		t.Errorf("nil cfg 应回 nil，got = %v", got)
	}
}

// === 出站装配（含「头只来自服务端配置」的负例） ===

func TestBuildProtocolRequest_AssemblesResponsesEndpoint(t *testing.T) {
	origValidate := validateURL
	validateURL = func(string) error { return nil }
	defer func() { validateURL = origValidate }()
	origUUID := newHeaderUUID
	newHeaderUUID = func() string { return "sess-1" }
	defer func() { newHeaderUUID = origUUID }()

	svc := &Service{}
	provider := &aimodel.AIProviderEntity{
		ProviderKey: "opencode-go",
		BaseURL:     "https://opencode.ai/zen/go/v1",
		Protocol:    aienums.ProtocolOpenAIResponses,
		ConfigData: aimodel.JSONMap{"headers": map[string]any{
			"x-opencode-session": "{{uuid}}",
		}},
	}
	req, err := svc.buildProtocolRequest(context.Background(), provider, protocolRequestInput{Model: "muse-spark-1.3-contributor", Messages: userMsgs("ping"), MaxOutputTokens: 16})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if req.URL.Path != "/zen/go/v1/responses" {
		t.Errorf("path = %q", req.URL.Path)
	}
	if req.Method != http.MethodPost {
		t.Errorf("method = %q", req.Method)
	}
	if got := req.Header.Get("x-opencode-session"); got != "sess-1" {
		t.Errorf("session = %q", got)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
}

// TestBuildProtocolRequest_HeadersOnlyFromProviderConfig 是「调用方不能注入头」的负例：
// 没有配置头时，请求上除固定集合外不能出现任何额外头（调用方拿不到注入通道）。
func TestBuildProtocolRequest_HeadersOnlyFromProviderConfig(t *testing.T) {
	origValidate := validateURL
	validateURL = func(string) error { return nil }
	defer func() { validateURL = origValidate }()

	svc := &Service{}
	provider := &aimodel.AIProviderEntity{
		ProviderKey: "opencode-go",
		BaseURL:     "https://opencode.ai/zen/go/v1",
		Protocol:    aienums.ProtocolOpenAIResponses,
	}
	req, err := svc.buildProtocolRequest(context.Background(), provider, protocolRequestInput{Model: "m", Messages: userMsgs("ping")})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	for name := range req.Header {
		switch http.CanonicalHeaderKey(name) {
		case "Content-Type", "Accept":
		default:
			t.Errorf("非配置来源的请求头出现了：%s", name)
		}
	}
	if req.Header.Get("Authorization") != "" {
		t.Error("无密钥时不应带 Authorization")
	}
}

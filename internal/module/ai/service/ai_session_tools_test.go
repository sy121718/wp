package aiservice_test

import (
	"cmp"
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
)

type stubToolProvider struct {
	specs  []aidto.ToolSpec
	text   string
	status aienums.ToolCallStatus
	err    error
	runs   []string
}

func (s *stubToolProvider) Specs() []aidto.ToolSpec { return s.specs }

func (s *stubToolProvider) Run(_ context.Context, _ int64, name, arguments string) (aiservice.ToolRunResult, error) {
	s.runs = append(s.runs, name+" "+arguments)
	if s.err != nil {
		return aiservice.ToolRunResult{}, s.err
	}
	return aiservice.ToolRunResult{Text: s.text, Status: s.status}, nil
}

func ordersSummarySpec() aidto.ToolSpec {
	return aidto.ToolSpec{
		Name:        "orders_summary",
		Description: "订单区间摘要",
		Parameters:  []byte(`{"type":"object","properties":{"projectId":{"type":"string"}},"required":["projectId"]}`),
	}
}

func responsesFunctionCall(callID, tool, args string) string {
	return `{"object":"response","status":"completed","output":[{"type":"function_call","call_id":"` +
		callID + `","name":"` + tool + `","arguments":` + jsonString(args) + `}]}`
}

func responsesAssistantText(text string) string {
	return `{"object":"response","status":"completed","output":[{"type":"message","role":"assistant",` +
		`"content":[{"type":"output_text","text":` + jsonString(text) + `}]}]}`
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func sessionChatUpstreamSeq(t *testing.T, svc *aiservice.Service, payloads ...string) <-chan string {
	t.Helper()
	bodies := make(chan string, 16)
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies <- string(raw)
		body := payloads[len(payloads)-1]
		if idx < len(payloads) {
			body = payloads[idx]
			idx++
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	addr := srv.Listener.Addr().String()
	svc.SetHTTPClient(&http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}})
	return bodies
}

func sendWithTools(t *testing.T, sess *aiservice.SessionService, key string) *aidto.SendMessageResult {
	t.Helper()
	res, err := sess.SendMessage(context.Background(), aidto.SendMessageReq{
		SessionKey:  key,
		ProviderKey: "sess-tools",
		Model:       "muse-spark-1.3-contributor",
		Input:       "今天有多少单",
		UserID:      testUserID,
	})
	if err != nil {
		t.Fatalf("发消息失败：%v", err)
	}
	return res
}

func TestSessionChatToolRoundTrip(t *testing.T) {
	sess, svc := newSessionChatService(t)
	newChatProvider(t, svc, "sess-tools", aienums.ProtocolOpenAIResponses)
	tools := &stubToolProvider{specs: []aidto.ToolSpec{ordersSummarySpec()}, text: "区间内 120 单"}
	sess.SetToolProvider(tools)

	bodies := sessionChatUpstreamSeq(t, svc,
		responsesFunctionCall("call_1", "orders_summary", `{"projectId":"p1"}`),
		`{"object":"response","status":"completed","output_text":"今天有 120 单"}`,
	)

	res := sendWithTools(t, sess, "tools-key-1")

	if len(tools.runs) != 1 {
		t.Fatalf("工具应被调用 1 次，实际 %v", tools.runs)
	}
	if tools.runs[0] != `orders_summary {"projectId":"p1"}` {
		t.Fatalf("工具入参被改动：%q", tools.runs[0])
	}

	first := waitBody(t, bodies)
	if !strings.Contains(first, `"tools"`) || !strings.Contains(first, "orders_summary") {
		t.Fatalf("第一次请求应带工具声明：%s", first)
	}
	second := waitBody(t, bodies)
	for _, want := range []string{`"function_call"`, `"call_id":"call_1"`, `"function_call_output"`, "区间内 120 单"} {
		if !strings.Contains(second, want) {
			t.Fatalf("第二次请求应含 %s，实得：%s", want, second)
		}
	}

	events := listSessionEvents(t, sess, res.Session.ID)
	slices.SortFunc(events, func(a, b aidto.SessionEventItem) int { return cmp.Compare(a.Seq, b.Seq) })
	got := make([]string, 0, len(events))
	for _, e := range events {
		got = append(got, e.Kind)
	}
	want := []string{"user", "tool", "tool", "assistant"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("事件序列 = %v，期望 %v", got, want)
	}
	if !strings.Contains(events[1].Content, "orders_summary") {
		t.Fatalf("调用事件的正文应写明调了哪个工具：%q", events[1].Content)
	}
	if events[2].Content != "区间内 120 单" {
		t.Fatalf("结果事件正文 = %q", events[2].Content)
	}

	if len(res.ToolEvents) != 2 || res.ToolEvents[0].Kind != "tool" {
		t.Fatalf("ToolEvents = %+v", res.ToolEvents)
	}
}

func TestSessionChatToolRoundsCapped(t *testing.T) {
	sess, svc := newSessionChatService(t)
	newChatProvider(t, svc, "sess-tools", aienums.ProtocolOpenAIResponses)
	tools := &stubToolProvider{specs: []aidto.ToolSpec{ordersSummarySpec()}, text: "又一轮"}
	sess.SetToolProvider(tools)

	bodies := sessionChatUpstreamSeq(t, svc, responsesFunctionCall("call_x", "orders_summary", `{}`))

	_, err := sess.SendMessage(context.Background(), aidto.SendMessageReq{
		SessionKey:  "tools-key-cap",
		ProviderKey: "sess-tools",
		Model:       "muse-spark-1.3-contributor",
		Input:       "一直查",
		UserID:      testUserID,
	})
	if !errors.Is(err, aiservice.ErrSessionToolRoundsExceeded) {
		t.Fatalf("应回轮次超限，实得 %v", err)
	}

	events := listSessionEvents(t, sess, 1)
	toolEvents := 0
	for _, e := range events {
		if e.Kind == "tool" {
			toolEvents++
		}
	}
	if toolEvents != 20 {
		t.Fatalf("工具事件应为 10 轮 × 2 = 20 条，实际 %d 条（事件：%d）", toolEvents, len(events))
	}

	for _, e := range events {
		if e.Kind == "assistant" {
			t.Fatal("超限时不该写入 assistant 事件")
		}
	}

	var last string
	for {
		select {
		case b := <-bodies:
			last = b
			continue
		default:
		}
		break
	}
	if last == "" {
		t.Fatal("没有捕获到末轮请求体")
	}
	if strings.Contains(last, `"tools"`) {
		t.Errorf("末轮必须收回工具，实得请求体：%s", last)
	}
}

func TestSessionChatManyToolsStillAnswers(t *testing.T) {
	sess, svc := newSessionChatService(t)
	newChatProvider(t, svc, "sess-tools", aienums.ProtocolOpenAIResponses)
	tools := &stubToolProvider{specs: []aidto.ToolSpec{ordersSummarySpec()}, text: "查到了"}
	sess.SetToolProvider(tools)

	payloads := make([]string, 0, 6)
	for i := 0; i < 5; i++ {
		payloads = append(payloads, responsesFunctionCall("call_x", "orders_summary", `{}`))
	}
	payloads = append(payloads, responsesAssistantText("今天 5 单"))
	sessionChatUpstreamSeq(t, svc, payloads...)

	res, err := sess.SendMessage(context.Background(), aidto.SendMessageReq{
		SessionKey:  "tools-key-many",
		ProviderKey: "sess-tools",
		Model:       "muse-spark-1.3-contributor",
		Input:       "今天有多少单",
		UserID:      testUserID,
	})
	if err != nil {
		t.Fatalf("5 步任务不该失败：%v", err)
	}
	if !strings.Contains(res.AssistantEvent.Content, "今天 5 单") {
		t.Errorf("没有拿到最终回答：%+v", res.AssistantEvent)
	}
	if len(res.ToolEvents) != 10 {
		t.Errorf("5 次工具往返应有 10 条事件，实得 %d", len(res.ToolEvents))
	}
}

func TestSessionChatToolFailureKeepsConversation(t *testing.T) {
	sess, svc := newSessionChatService(t)
	newChatProvider(t, svc, "sess-tools", aienums.ProtocolOpenAIResponses)
	tools := &stubToolProvider{
		specs: []aidto.ToolSpec{ordersSummarySpec()},
		err:   errors.New("db down: password=xxx"),
	}
	sess.SetToolProvider(tools)

	bodies := sessionChatUpstreamSeq(t, svc,
		responsesFunctionCall("call_1", "orders_summary", `{}`),
		`{"object":"response","status":"completed","output_text":"查询失败了，请稍后再试"}`,
	)

	res := sendWithTools(t, sess, "tools-key-fail")

	_ = waitBody(t, bodies)
	second := waitBody(t, bodies)

	if strings.Contains(second, "db down") || strings.Contains(second, "password") {
		t.Fatalf("工具失败原文不该回灌给模型：%s", second)
	}
	if !strings.Contains(second, "tool orders_summary failed: status=failed, error="+aienums.ErrToolRunFailed) {
		t.Fatalf("应回灌工具失败事实，实得：%s", second)
	}

	events := listSessionEvents(t, sess, res.Session.ID)
	slices.SortFunc(events, func(a, b aidto.SessionEventItem) int { return cmp.Compare(a.Seq, b.Seq) })
	if events[2].Content != "tool orders_summary failed: status=failed, error="+aienums.ErrToolRunFailed {
		t.Fatalf("结果事件正文 = %q", events[2].Content)
	}
}

func TestSessionChatWithoutToolsOmitsToolsField(t *testing.T) {
	sess, svc := newSessionChatService(t)
	newChatProvider(t, svc, "sess-tools", aienums.ProtocolOpenAIResponses)
	bodies := sessionChatUpstreamSeq(t, svc, `{"object":"response","status":"completed","output_text":"好的"}`)

	sendWithTools(t, sess, "tools-key-none")

	body := waitBody(t, bodies)
	if strings.Contains(body, `"tools"`) {
		t.Fatalf("未接工具时不该带 tools：%s", body)
	}
	if strings.Contains(body, "orders_summary") {
		t.Fatalf("未接工具时不该出现工具名：%s", body)
	}
}

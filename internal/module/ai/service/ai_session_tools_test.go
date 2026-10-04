package aiservice_test

// ai_session_tools_test.go — 会话页「发消息」的工具往返（agent loop）。
//
// 这一层钉住的是**一次请求内部的来回**：
//   - 模型要求调工具时，工具被真的执行（入参原样透传）；
//   - 「调用 / 结果」成对落进事件日志，并把结果回灌给模型的下一轮请求；
//   - 模型给不出正文、一直要调工具时，轮次上限会兜住（不把额度烧光）；
//   - 工具执行失败**不中断对话**：模型拿到一句失败文案，仍要给出回答；
//   - 没接工具时出站请求不带 tools 字段（一问一答的老路径一字不变）。
//
// PG 不可用时用例整体 t.Skip（support 的一致口径）。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
)

// stubToolProvider 假工具端口：记录调用、回固定文本。
type stubToolProvider struct {
	specs []aidto.ToolSpec
	text  string
	err   error
	runs  []string
}

func (s *stubToolProvider) Specs() []aidto.ToolSpec { return s.specs }

func (s *stubToolProvider) Run(_ context.Context, _ int64, name, arguments string) (string, error) {
	s.runs = append(s.runs, name+" "+arguments)
	return s.text, s.err
}

// ordersSummarySpec 一条工具声明（内容与真实 tools 无关，用例只验证「传下去了」）。
func ordersSummarySpec() aidto.ToolSpec {
	return aidto.ToolSpec{
		Name:        "orders_summary",
		Description: "订单区间摘要",
		Parameters:  []byte(`{"type":"object","properties":{"projectId":{"type":"string"}},"required":["projectId"]}`),
	}
}

// responsesFunctionCall 一份「只要求调工具、没有正文」的 responses 响应。
//
// 这份载荷本身就是本批要钉住的既有缺陷：正文为空在过去被判成 ErrInternal，
// 而模型要调工具时正文本来就是空的。
func responsesFunctionCall(callID, tool, args string) string {
	return `{"object":"response","status":"completed","output":[{"type":"function_call","call_id":"` +
		callID + `","name":"` + tool + `","arguments":` + jsonString(args) + `}]}`
}

// jsonString 把一段文本编成 JSON 字符串字面量（避免在用例里手写转义）。
func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// sessionChatUpstreamSeq 起一个按**请求次序**回不同载荷的假上游；用完后重复最后一个。
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

// sendWithTools 发一条消息（用例里重复出现的入参）。
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

// TestSessionChatToolRoundTrip 一轮工具往返：执行 → 成对落库 → 回灌 → 模型作答。
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

	// ① 工具被真的执行，且入参原样透传。
	if len(tools.runs) != 1 {
		t.Fatalf("工具应被调用 1 次，实际 %v", tools.runs)
	}
	if tools.runs[0] != `orders_summary {"projectId":"p1"}` {
		t.Fatalf("工具入参被改动：%q", tools.runs[0])
	}

	// ② 两次出站：第二次必须带着「调用 + 结果」两条原生消息（缺一条上游会拒）。
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

	// ③ 事件成对落库：user → tool(call) → tool(result) → assistant。
	// ListEvents 按 seq **倒序**回（列表页要从新到旧），这里排序后再断言时序关系。
	events := listSessionEvents(t, sess, res.Session.ID)
	sort.Slice(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
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

	// ④ 返回里带上工具事件（页面要逐条显示，不能只回最终答复）。
	if len(res.ToolEvents) != 2 || res.ToolEvents[0].Kind != "tool" {
		t.Fatalf("ToolEvents = %+v", res.ToolEvents)
	}
}

// TestSessionChatToolRoundsCapped 模型一直要调工具 → 撞上轮次上限，但事件全部保留。
func TestSessionChatToolRoundsCapped(t *testing.T) {
	sess, svc := newSessionChatService(t)
	newChatProvider(t, svc, "sess-tools", aienums.ProtocolOpenAIResponses)
	tools := &stubToolProvider{specs: []aidto.ToolSpec{ordersSummarySpec()}, text: "又一轮"}
	sess.SetToolProvider(tools)

	// 假上游永远只回「要调工具」——这正是需要上限兜住的形态。
	bodies := sessionChatUpstreamSeq(t, svc, responsesFunctionCall("call_x", "orders_summary", `{}`))
	_ = bodies

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

	// 上限是 4 轮：每轮一次调用 + 一次结果，事件必须都在（审计要看到它试了什么）。
	events := listSessionEvents(t, sess, 1)
	toolEvents := 0
	for _, e := range events {
		if e.Kind == "tool" {
			toolEvents++
		}
	}
	if toolEvents != 8 {
		t.Fatalf("工具事件应为 4 轮 × 2 = 8 条，实际 %d 条（事件：%d）", toolEvents, len(events))
	}
	// 没有 assistant 事件：这一轮没有答案。
	for _, e := range events {
		if e.Kind == "assistant" {
			t.Fatal("超限时不该写入 assistant 事件")
		}
	}
}

// TestSessionChatToolFailureKeepsConversation 工具执行失败不中断对话：
// 模型拿到失败文案，仍要给出回答。
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

	_ = waitBody(t, bodies)       // 第一轮：只发历史 + 工具声明
	second := waitBody(t, bodies) // 第二轮：带「调用 + 失败结果」
	// 失败原文**不能**进上下文：它可能带连接串、表名，而它会经模型的嘴出现在页面上。
	if strings.Contains(second, "db down") || strings.Contains(second, "password") {
		t.Fatalf("工具失败原文不该回灌给模型：%s", second)
	}
	if !strings.Contains(second, "工具执行失败") {
		t.Fatalf("应回灌归口文案，实得：%s", second)
	}

	events := listSessionEvents(t, sess, res.Session.ID)
	sort.Slice(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
	if events[2].Content != "工具执行失败" {
		t.Fatalf("结果事件正文 = %q", events[2].Content)
	}
}

// TestSessionChatWithoutToolsOmitsToolsField 没接工具时按一问一答走：请求里不能出现 tools。
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

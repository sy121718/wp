package aiservice_test

// ai_session_chat_test.go — 会话页「发消息」的 service 链路（真上游换 httptest 假上游）。
//
// 外部测试包与 ai_chat_test.go 同口径（内部测试包 import support 会成环），
// 复用它的 newChatTestService / newChatProvider / fakeUpstream / awaitHit。
//
// 这一层钉住「一次发消息落成什么样的状态」：
//   - 只给会话键时自动建会话，并把用户输入与模型回复各自写成一条事件；
//   - 发给上游的输入里带着当前上下文投影（不是只发本条消息）；
//   - 上游失败时用户那条已经落库、assistant 那条不写（失败不留半截记录）；
//   - 上游回空文本归口 ErrSessionChatEmptyReply；
//   - 没注入对话端口时回 ErrSessionChatUnavailable（不 panic）。
//
// PG 不可用时用例整体 t.Skip（support 的一致口径）。

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
)

// newSessionChatService 在同一个迁移库上建会话服务，并把配置面的 Service 当对话端口接上。
func newSessionChatService(t *testing.T) (*aiservice.SessionService, *aiservice.Service) {
	t.Helper()
	svc, db := newChatTestService(t)
	sess := aiservice.NewSessionService(aimodel.NewSessionModel(db))
	sess.SetChatPort(svc)
	return sess, svc
}

// sessionChatUpstream 起一个假上游：把请求正文回传出来（用于断言「投影参与了拼装」）。
// 出站客户端同样走「改写拨号目标」的路子，不依赖 DNS。
func sessionChatUpstream(t *testing.T, svc *aiservice.Service, reply string) <-chan string {
	t.Helper()
	bodies := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies <- string(raw)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"response","output_text":"` + reply + `"}`))
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

// testUserID 用例里的「已登录账号」：会话层的第一关卡要求 UserID > 0，
// 每个请求字面量都要带上它（没有身份 = 不受理，这正是要钉住的行为）。
const testUserID int64 = 1

// waitBody 取假上游收到的请求正文（超时捏死，避免用例挂住）。
func waitBody(t *testing.T, bodies <-chan string) string {
	t.Helper()
	select {
	case b := <-bodies:
		return b
	case <-time.After(3 * time.Second):
		t.Fatal("假上游没有收到请求")
		return ""
	}
}

// listSessionEvents 读回一条会话的事件日志（升序段取足够大）。
func listSessionEvents(t *testing.T, sess *aiservice.SessionService, id int64) []aidto.SessionEventItem {
	t.Helper()
	events, _, err := sess.ListEvents(context.Background(), id, 1, 100)
	if err != nil {
		t.Fatalf("读事件失败：%v", err)
	}
	return events
}

// TestSessionChatSendCreatesSessionAndWritesBothEvents 只给会话键 → 自动建会话，落 user + assistant 两条事件。
func TestSessionChatSendCreatesSessionAndWritesBothEvents(t *testing.T) {
	sess, svc := newSessionChatService(t)
	bodies := sessionChatUpstream(t, svc, "你好")
	newChatProvider(t, svc, "sess-chat", aienums.ProtocolOpenAIResponses)

	res, err := sess.SendMessage(context.Background(), aidto.SendMessageReq{
		SessionKey:  "chat-key-1",
		ProviderKey: "sess-chat",
		Model:       "muse",
		Input:       "在吗",
		UserID:      testUserID,
	})
	if err != nil {
		t.Fatalf("发消息失败：%v", err)
	}
	if res.Session.ID <= 0 {
		t.Fatalf("没有落到会话：%+v", res.Session)
	}
	if res.UserEvent.Kind != string(aienums.EventKindUser) || res.AssistantEvent.Kind != string(aienums.EventKindAssistant) {
		t.Fatalf("两条事件的类型不对：%q / %q", res.UserEvent.Kind, res.AssistantEvent.Kind)
	}
	if res.AssistantEvent.Content != "你好" {
		t.Fatalf("模型回复没落库：%q", res.AssistantEvent.Content)
	}
	if body := waitBody(t, bodies); !strings.Contains(body, "在吗") {
		t.Fatalf("上游没收到用户输入：%s", body)
	}
	if events := listSessionEvents(t, sess, res.Session.ID); len(events) != 2 {
		t.Fatalf("事件条数 = %d，想要 2", len(events))
	}
}

// TestSessionChatSendSendsProjectionToUpstream 历史事件以 <role>: <content> 形式进上游输入。
func TestSessionChatSendSendsProjectionToUpstream(t *testing.T) {
	sess, svc := newSessionChatService(t)
	bodies := sessionChatUpstream(t, svc, "收到")
	newChatProvider(t, svc, "sess-chat", aienums.ProtocolOpenAIResponses)

	head, err := sess.EnsureSession(context.Background(), "chat-key-2", "sess-chat", "muse", "历史会话", testUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	if _, err := sess.AppendEvent(context.Background(), aidto.AppendEventReq{
		SessionID: head.ID,
		Kind:      string(aienums.EventKindUser),
		Content:   "上一条问题",
		UserID:    testUserID,
	}); err != nil {
		t.Fatalf("追加历史事件失败：%v", err)
	}

	if _, err := sess.SendMessage(context.Background(), aidto.SendMessageReq{
		SessionID:   head.ID,
		ProviderKey: "sess-chat",
		Model:       "muse",
		Input:       "接着问",
		UserID:      testUserID,
	}); err != nil {
		t.Fatalf("发消息失败：%v", err)
	}

	body := waitBody(t, bodies)
	if !strings.Contains(body, "上一条问题") || !strings.Contains(body, "接着问") {
		t.Fatalf("投影没进输入：%s", body)
	}
	if !strings.Contains(body, "user: ") {
		t.Fatalf("输入不是 <role>: <content> 形态：%s", body)
	}
}

// TestSessionChatSendKeepsUserEventWhenUpstreamFails 上游 5xx → 只留 user 事件，不写 assistant。
func TestSessionChatSendKeepsUserEventWhenUpstreamFails(t *testing.T) {
	sess, svc := newSessionChatService(t)
	hits := fakeUpstream(t, svc, http.StatusInternalServerError, `{"error":"boom"}`)
	newChatProvider(t, svc, "sess-chat", aienums.ProtocolOpenAIResponses)

	head, err := sess.EnsureSession(context.Background(), "chat-key-3", "sess-chat", "muse", "失败会话", testUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	res, err := sess.SendMessage(context.Background(), aidto.SendMessageReq{
		SessionID:   head.ID,
		ProviderKey: "sess-chat",
		Model:       "muse",
		Input:       "会失败",
		UserID:      testUserID,
	})
	if err == nil {
		t.Fatal("上游 5xx 时不该返回成功")
	}
	if res != nil {
		t.Fatalf("失败时不该有结果：%+v", res)
	}
	awaitHit(t, hits)

	events := listSessionEvents(t, sess, head.ID)
	if len(events) != 1 {
		t.Fatalf("事件条数 = %d，想要 1（只留 user）", len(events))
	}
	if events[0].Kind != string(aienums.EventKindUser) {
		t.Fatalf("留下的不是 user 事件：%q", events[0].Kind)
	}
}

// TestSessionChatSendRejectsEmptyReply 上游回空文本 → 报错且不写 assistant 事件。
//
// 注意「谁先拒绝」有两道：已经落盘的 Chat 实现在解析阶段就把空响应归口成错误
// （所以这里拿到的是它自己的 key）；SendMessage 里的 ErrSessionChatEmptyReply 是第二道，
// 防的是「换一个 ChatPort 实现回来一段空字符串但不报错」。本用例只钉住外部可观察的结果。
func TestSessionChatSendRejectsEmptyReply(t *testing.T) {
	sess, svc := newSessionChatService(t)
	_ = sessionChatUpstream(t, svc, "")
	newChatProvider(t, svc, "sess-chat", aienums.ProtocolOpenAIResponses)

	head, err := sess.EnsureSession(context.Background(), "chat-key-4", "sess-chat", "muse", "空回复", testUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	if _, err := sess.SendMessage(context.Background(), aidto.SendMessageReq{
		SessionID:   head.ID,
		ProviderKey: "sess-chat",
		Model:       "muse",
		Input:       "空回复",
		UserID:      testUserID,
	}); err == nil {
		t.Fatal("上游回空文本时不该返回成功")
	}
	if events := listSessionEvents(t, sess, head.ID); len(events) != 1 {
		t.Fatalf("事件条数 = %d，想要 1（空回复不落库）", len(events))
	}
}

// TestSessionChatSendWithoutChatPortFails 没注入对话端口 → 明确错误，不 panic。
func TestSessionChatSendWithoutChatPortFails(t *testing.T) {
	_, db := newChatTestService(t)
	sess := aiservice.NewSessionService(aimodel.NewSessionModel(db))

	_, err := sess.SendMessage(context.Background(), aidto.SendMessageReq{
		SessionKey:  "chat-key-5",
		ProviderKey: "sess-chat",
		Model:       "muse",
		Input:       "hi",
		UserID:      testUserID,
	})
	if !errors.Is(err, aiservice.ErrSessionChatUnavailable) {
		t.Fatalf("err = %v，想要 ErrSessionChatUnavailable", err)
	}
}

// TestSessionChatSendValidatesInput 空输入 / 缺供应商模型 → 各自的哨兵（不落到上游）。
func TestSessionChatSendValidatesInput(t *testing.T) {
	sess, _ := newSessionChatService(t)
	ctx := context.Background()

	if _, err := sess.SendMessage(ctx, aidto.SendMessageReq{SessionKey: "k", ProviderKey: "p", Model: "m", Input: "   ", UserID: testUserID}); !errors.Is(err, aiservice.ErrSessionChatInputRequired) {
		t.Fatalf("空输入 err = %v，想要 ErrSessionChatInputRequired", err)
	}
	if _, err := sess.SendMessage(ctx, aidto.SendMessageReq{SessionKey: "k", Input: "hi", UserID: testUserID}); !errors.Is(err, aiservice.ErrSessionChatModelRequired) {
		t.Fatalf("缺模型 err = %v，想要 ErrSessionChatModelRequired", err)
	}
}

// TestSessionChatSendRequiresUser 没有身份 → 第一关卡直接拒，**连对话端口都不该被调用**。
//
// 这一条钉住「准入条件而不是事后记账」：把 UserID 判断挪到写事件之后、或挪到组装回复之后，
// 都会让匿名请求先把消息写进别人的会话再失败 —— 那时本用例会看到事件表多了一行。
func TestSessionChatSendRequiresUser(t *testing.T) {
	sess, svc := newSessionChatService(t)
	bodies := sessionChatUpstream(t, svc, "不该被调用")
	newChatProvider(t, svc, "sess-chat", aienums.ProtocolOpenAIResponses)

	if _, err := sess.SendMessage(context.Background(), aidto.SendMessageReq{
		SessionKey:  "chat-key-anon",
		ProviderKey: "sess-chat",
		Model:       "muse",
		Input:       "匿名请求",
	}); !errors.Is(err, aiservice.ErrUserRequired) {
		t.Fatalf("无身份 err = %v，想要 ErrUserRequired", err)
	}
	select {
	case body := <-bodies:
		t.Fatalf("无身份的请求不该打到上游，实际收到：%s", body)
	default:
	}
}

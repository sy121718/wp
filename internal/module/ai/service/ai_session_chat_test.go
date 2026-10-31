package aiservice_test

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
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
	aiprompt "go_wp/internal/module/ai/prompt"
	aiservice "go_wp/internal/module/ai/service"
)

func newSessionChatService(t *testing.T) (*aiservice.SessionService, *aiservice.Service) {
	t.Helper()
	svc, db := newChatTestService(t)
	sess := aiservice.NewSessionService(aimodel.NewSessionModel(db))
	sess.SetChatPort(svc)
	return sess, svc
}

func sessionChatUpstream(t *testing.T, svc *aiservice.Service, reply string) <-chan string {
	t.Helper()
	bodies := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies <- string(raw)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		_, _ = w.Write([]byte(`{"object":"response","output_text":"` + reply + `",` +
			`"choices":[{"message":{"role":"assistant","content":"` + reply + `"}}]}`))
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

const testUserID int64 = 1

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

func listSessionEvents(t *testing.T, sess *aiservice.SessionService, id int64) []aidto.SessionEventItem {
	t.Helper()
	events, _, err := sess.ListEvents(context.Background(), id, 1, 100)
	if err != nil {
		t.Fatalf("读事件失败：%v", err)
	}
	return events
}

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

func TestSessionChatSendsSystemPrefixFirst(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
	}{
		{"chat_completions", aienums.ProtocolOpenAIChatCompletions},
		{"responses", aienums.ProtocolOpenAIResponses},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess, svc := newSessionChatService(t)
			bodies := sessionChatUpstream(t, svc, "在的")
			newChatProvider(t, svc, "sess-prefix", tc.protocol)

			if _, err := sess.SendMessage(context.Background(), aidto.SendMessageReq{
				SessionKey:  "prefix-key-" + tc.name,
				ProviderKey: "sess-prefix",
				Model:       "muse",
				Input:       "这周卖得最好的是什么",
				UserID:      testUserID,
			}); err != nil {
				t.Fatalf("发消息失败：%v", err)
			}

			body := waitBody(t, bodies)
			var payload struct {
				Instructions string `json:"instructions"`
				Messages     []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal([]byte(body), &payload); err != nil {
				t.Fatalf("请求正文不是 JSON：%v", err)
			}
			if tc.protocol == aienums.ProtocolOpenAIResponses {

				if !strings.HasPrefix(payload.Instructions, aiprompt.SiteRules()) {
					t.Errorf("responses 协议的常驻规则必须放在顶层 instructions 的开头，实得 %q", truncateForLog(payload.Instructions))
				}
				if catalog := aiprompt.ManualCatalog(); catalog != "" && !strings.Contains(payload.Instructions, catalog) {
					t.Error("手册目录没有进 instructions")
				}

				if strings.Contains(body, `"role":"system"`) {
					t.Error("responses 协议的 input[] 里出现了 system 条目 —— 上游不接受这个角色")
				}
				if !strings.Contains(body, "这周卖得最好的是什么") {
					t.Error("本轮输入不在 input 里")
				}
				return
			}
			if len(payload.Messages) < 2 {
				t.Fatalf("请求里只有 %d 条消息，至少要有 system + user", len(payload.Messages))
			}
			if payload.Messages[0].Role != "system" {
				t.Errorf("第一条必须是 system（排在后面就不是前缀，缓存命中不了），实得 %q", payload.Messages[0].Role)
			}
			if payload.Messages[0].Content != aiprompt.SiteRules() {
				t.Error("system 的内容必须就是常驻规则原文")
			}

			last := payload.Messages[len(payload.Messages)-1]
			if last.Role != "user" || !strings.Contains(last.Content, "这周卖得最好的是什么") {
				t.Errorf("最后一条应是带本轮输入的 user 消息，实得 %q/%q", last.Role, truncateForLog(last.Content))
			}

			if catalog := aiprompt.ManualCatalog(); catalog != "" {
				joined := ""
				for _, m := range payload.Messages {
					joined += m.Content
				}
				if !strings.Contains(joined, catalog) {
					t.Error("手册目录没有进 messages")
				}
			}
		})
	}
}

func truncateForLog(s string) string {
	r := []rune(s)
	if len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

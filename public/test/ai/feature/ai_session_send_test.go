package feature

import (
	pageview "go_wp/pkg/view"
	"context"
	"go_wp/internal/adminui"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/pkg/i18n"
	"go_wp/pkg/response"

	"go_wp/internal/middleware/adminctx"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/templates"
)

const sessionSendBaseURL = "http://198.51.100.7"

func newSessionSendEnv(t *testing.T) (*aihttp.SessionHandle, *aiservice.SessionService, *aiservice.Service, *gorm.DB) {
	t.Helper()
	svc, db := newAIProviderService(t)
	sess := aiservice.NewSessionService(aimodel.NewSessionModel(db))
	sess.SetChatPort(svc)

	callLog := aimodel.NewCallLogModel(db)
	svc.SetCallLogWriter(callLog)
	sess.SetCallLogReader(callLog)
	// 续写已有会话只有一个入口：POST /api/ai/session/send（页面侧的 /admin 写路由已清退）。
	return aihttp.NewSessionHandle(sess), sess, svc, db
}

func sessionSendUpstream(t *testing.T, svc *aiservice.Service, reply string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}

func newSessionSendProvider(t *testing.T, svc *aiservice.Service) {
	t.Helper()
	if _, err := svc.SaveProvider(context.Background(), &aidto.SaveProviderReq{
		ProviderKey: "sess-page",
		DisplayName: "会话页测试供应商",
		BaseURL:     sessionSendBaseURL,
		Protocol:    aienums.ProtocolOpenAIResponses,
		APIKey:      "sk-session-page-0123456789abcdef",
	}); err != nil {
		t.Fatalf("建供应商失败：%v", err)
	}
}

const sessionSendUserID int64 = 1

func serveSessionSend(t *testing.T, h *aihttp.SessionHandle, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		c.Set("user_id", sessionSendUserID)
		c.Next()
	})
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.POST("/api/ai/session/send", h.Send)
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestAISessionSendPageRedirectsAndAppendsTwoEvents(t *testing.T) {
	ph, sess, svc, _ := newSessionSendEnv(t)
	sessionSendUpstream(t, svc, "模型回复")
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(context.Background(), "send-key-1", "sess-page", "muse", "会话", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	rec := serveSessionSend(t, ph, "/api/ai/session/send",
		"csrf_token=test-token&sessionId="+strconv.FormatInt(head.ID, 10)+"&providerKey=sess-page&model=muse&input=你好")
	assertAIOK(t, rec, "消息已发送，模型已回复")

	events, _, err := sess.ListEvents(context.Background(), head.ID, 1, 100)
	if err != nil {
		t.Fatalf("读事件失败：%v", err)
	}
	if len(events) != 2 {
		t.Fatalf("事件条数 = %d，想要 2", len(events))
	}
}

func sessionSendUpstreamJSON(t *testing.T, svc *aiservice.Service, status int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	addr := srv.Listener.Addr().String()
	svc.SetHTTPClient(&http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}})
}

func awaitCallLog(t *testing.T, db *gorm.DB, sessionID int64) aimodel.AICallLogEntity {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		var row aimodel.AICallLogEntity
		last = db.WithContext(context.Background()).Model(&aimodel.AICallLogEntity{}).
			Where("session_id = ?", sessionID).Order("id DESC").Take(&row).Error
		if last == nil {
			return row
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("5 秒内没有等到调用流水落库：%v", last)
	return aimodel.AICallLogEntity{}
}

func TestAISessionSendWritesCallLog(t *testing.T) {
	ph, sess, svc, db := newSessionSendEnv(t)
	sessionSendUpstreamJSON(t, svc, http.StatusOK,
		`{"object":"response","output_text":"模型回复","usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`)
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(context.Background(), "send-log-1", "sess-page", "muse", "会话", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	rec := serveSessionSend(t, ph, "/api/ai/session/send",
		"csrf_token=test-token&sessionId="+strconv.FormatInt(head.ID, 10)+"&providerKey=sess-page&model=muse&input=你好")
	assertAIOK(t, rec, "消息已发送，模型已回复")

	row := awaitCallLog(t, db, head.ID)
	if row.Status != string(aienums.CallStatusOK) {
		t.Fatalf("status = %q，想要 ok（error_key=%q）", row.Status, row.ErrorKey)
	}
	if row.UserID != sessionSendUserID {
		t.Fatalf("user_id = %d，想要 %d —— 「谁调用的」必须落库", row.UserID, sessionSendUserID)
	}
	if row.SessionID != head.ID {
		t.Fatalf("session_id = %d，想要 %d", row.SessionID, head.ID)
	}
	if row.ProviderKey != "sess-page" || row.ModelID != "muse" {
		t.Fatalf("来源不对：%s / %s", row.ProviderKey, row.ModelID)
	}
	if !row.UsageReported || row.InputTokens != 11 || row.OutputTokens != 7 || row.TotalTokens != 18 {
		t.Fatalf("用量不对：%+v", row)
	}
	if row.LatencyMs < 0 {
		t.Fatalf("耗时不该为负：%d", row.LatencyMs)
	}
}

func serveSessionsPage(t *testing.T, ph *aihttp.SessionPageHandle) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("user_id", sessionSendUserID)
		c.Set(adminctx.PermSetKey, map[string]bool{
			"ai:provider_list": true, "ai:session_list": true, "ai:chat": true,
		})
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"ai.provider_list": true, "ai.session_list": true, "ai.chat": true,
		})

		c.Next()
	})
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.GET("/admin/ai/sessions", pageview.Slot(aihttp.SessionsPageTemplate, adminui.RenderPage), ph.SessionsPage)

	req := httptest.NewRequest(http.MethodGet, "/admin/ai/sessions", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestAISessionSendPageShowsRecentCalls(t *testing.T) {
	ph, sess, svc, db := newSessionSendEnv(t)
	sessionSendUpstreamJSON(t, svc, http.StatusOK,
		`{"object":"response","output_text":"模型回复","usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`)
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(context.Background(), "send-log-3", "sess-page", "muse", "会话", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	rec := serveSessionSend(t, ph, "/api/ai/session/send",
		"csrf_token=test-token&sessionId="+strconv.FormatInt(head.ID, 10)+"&providerKey=sess-page&model=muse&input=你好")
	assertAIOK(t, rec, "消息已发送，模型已回复")

	awaitCallLog(t, db, head.ID)

	// 页面句柄必须复用本用例的 sess/svc/db：另建一套环境会连到另一份测试库，
	// 页面就看不到刚写进去的事件与调用流水。
	page := serveSessionsPage(t, aihttp.NewSessionPageHandle(sess, svc))
	if page.Code != http.StatusOK {
		t.Fatalf("会话页状态码 = %d，想要 200；body=%s", page.Code, page.Body.String())
	}
	body := page.Body.String()
	for _, want := range []string{"最近调用", "次调用", "muse", "成功"} {
		if !strings.Contains(body, want) {
			t.Fatalf("会话页的悬浮卡缺少 %q", want)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatal("整页没有渲染完（缺 </html>）：模板在某一行中断了")
	}
}

func TestAISessionSendWritesFailedCallLog(t *testing.T) {
	ph, sess, svc, db := newSessionSendEnv(t)
	sessionSendUpstreamJSON(t, svc, http.StatusInternalServerError, `{"error":"boom"}`)
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(context.Background(), "send-log-2", "sess-page", "muse", "会话", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	rec := serveSessionSend(t, ph, "/api/ai/session/send",
		"csrf_token=test-token&sessionId="+strconv.FormatInt(head.ID, 10)+"&providerKey=sess-page&model=muse&input=会失败")
	// 上游 500：服务端不把上游原文交给前端，只给归口文案 + 500。
	// 归口文案就是共享出口那一句（pkg/response.InternalFallback），逐字断言。
	failed := assertAIFail(t, rec, http.StatusInternalServerError, "")
	wantMessage := i18n.Translate(aienums.ErrInternal, response.InternalFallback, "zh-CN")
	if failed.Message != wantMessage {
		t.Fatalf("失败应给生产词库归口文案 %q，实际 %q", wantMessage, failed.Message)
	}

	row := awaitCallLog(t, db, head.ID)
	if row.Status != string(aienums.CallStatusError) {
		t.Fatalf("status = %q，想要 error", row.Status)
	}
	if row.ErrorKey != aienums.ErrInternal {
		t.Fatalf("error_key = %q，想要 %q（底层原文只进日志，不进表）", row.ErrorKey, aienums.ErrInternal)
	}
	if row.UsageReported {
		t.Fatalf("失败的调用不该有用量：%+v", row)
	}
	if events, _, err := sess.ListEvents(context.Background(), head.ID, 1, 100); err != nil || len(events) != 1 {
		t.Fatalf("失败时只该留 user 事件：n=%d err=%v", len(events), err)
	}
}

func TestAISessionSendPageRedirectsErrOnEmptyInput(t *testing.T) {
	ph, sess, svc, _ := newSessionSendEnv(t)
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(context.Background(), "send-key-2", "sess-page", "muse", "会话", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	rec := serveSessionSend(t, ph, "/api/ai/session/send",
		"csrf_token=test-token&sessionId="+strconv.FormatInt(head.ID, 10)+"&providerKey=sess-page&model=muse&input=")
	assertAIFail(t, rec, http.StatusBadRequest, "请输入消息内容")
	if events, _, err := sess.ListEvents(context.Background(), head.ID, 1, 100); err != nil || len(events) != 0 {
		t.Fatalf("空输入不该落事件：n=%d err=%v", len(events), err)
	}
}

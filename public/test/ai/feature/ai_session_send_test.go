package feature

// ai_session_send_test.go — 会话页「发消息」的 PRG 出口（模板 + handler）。
//
// 覆盖范围与 ai_page_test.go 同口径：直挂 handler（不含 SessionAuth / CSRF / Casbin 那条链），
// 钉住两件事：
//  1. 成功发消息走 PRG（303）回会话页，并且事件表真的多两行（user + assistant）；
//  2. 校验失败同样走 303，但提示以 i18n key 形式挂在 query 的 err 槽上。
//
// 上游用 httptest 假服务：BaseURL 取 TEST-NET-2 字面量过 SSRF 门禁，
// 出站客户端改写拨号目标，不依赖 DNS。

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/templates"
	"go_wp/internal/shell"
)

// sessionSendBaseURL 全局单播的 documentation 网段字面量（只为过 SSRF 门禁）。
const sessionSendBaseURL = "http://198.51.100.7"

// newSessionSendEnv 真 PG + 真种子 + 真会话服务（已接上配置面的对话端口与调用流水端口）。
//
// 装配形状与生产一致（见 ai_router.go）：对话流水挂在**出站层**，
// 所以这里注入的是 svc 而不是 sess；db 一并交回，供用例断言流水真的落了库。
func newSessionSendEnv(t *testing.T) (*aihttp.SessionPageHandle, *aiservice.SessionService, *aiservice.Service, *gorm.DB) {
	t.Helper()
	svc, db := newAIProviderService(t)
	sess := aiservice.NewSessionService(aimodel.NewSessionModel(db))
	sess.SetChatPort(svc)
	// 调用流水一个实例两个方向，与 ai_router.go 的装配逐条对齐：
	// 写侧挂出站层（Service.Chat 记账），读侧挂会话层（悬浮卡的「最近调用」）。
	callLog := aimodel.NewCallLogModel(db)
	svc.SetCallLogWriter(callLog)
	sess.SetCallLogReader(callLog)
	return aihttp.NewSessionPageHandle(sess, svc), sess, svc, db
}

// sessionSendUpstream 起假上游并把 service 的出站客户端接过去（忽略 URL host）。
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

// newSessionSendProvider 建一个已启用的供应商（密钥走加密入库）。
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

// sessionSendUserID 用例里的「已登录账号」：页面 handler 从 gin.Context 取 user_id
// （shell.CurrentUserID → builtin.GetUserID），所以中间件往 context 里塞一个。
// 这里不塞就等于未登录 —— 会话层的第一关卡会把请求挡在对话之前（ErrUserRequired）。
const sessionSendUserID int64 = 1

// serveSessionSend 把请求交给会话页 handler（真实 Jet 引擎，只有 send 这一条写路由）。
func serveSessionSend(t *testing.T, ph *aihttp.SessionPageHandle, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 只补 user_id 这一件事：用例直挂 handler，不走 SessionAuth / CSRF / Casbin 那条链。
	engine.Use(func(c *gin.Context) {
		c.Set("user_id", sessionSendUserID)
		c.Next()
	})
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.POST("/admin/ai/sessions/send", ph.SessionSend)
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestAISessionSendPageRedirectsAndAppendsTwoEvents 发一条消息 → 303 回会话页，事件表多两行。
func TestAISessionSendPageRedirectsAndAppendsTwoEvents(t *testing.T) {
	ph, sess, svc, _ := newSessionSendEnv(t)
	sessionSendUpstream(t, svc, "模型回复")
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(context.Background(), "send-key-1", "sess-page", "muse", "会话", 0)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	rec := serveSessionSend(t, ph, "/admin/ai/sessions/send",
		"csrf_token=test-token&sessionId="+strconv.FormatInt(head.ID, 10)+"&providerKey=sess-page&model=muse&input=你好")
	assertAIJump(t, rec, true, "消息已发送，模型已回复")
	// 回跳不再带 id：详情已经是抽屉，那句 id 只会让列表页白查一次详情，而页面并不渲染它。
	if !strings.Contains(rec.Body.String(), `href="/admin/ai/sessions"`) {
		t.Fatalf("回跳地址不对：%s", rec.Body.String())
	}

	events, _, err := sess.ListEvents(context.Background(), head.ID, 1, 100)
	if err != nil {
		t.Fatalf("读事件失败：%v", err)
	}
	if len(events) != 2 {
		t.Fatalf("事件条数 = %d，想要 2", len(events))
	}
}

// sessionSendUpstreamJSON 起一个指定状态码 / 响应体的假上游（覆盖失败与 usage 两条路径）。
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

// awaitCallLog 等一条会话的调用流水落库（倒序取最新一条）。
//
// 流水是**协程异步写**的（见 service/ai_call_log.go），所以这里必须轮询等待 ——
// 而「等得到」本身就是这条用例要钉住的性质：脱离请求 ctx 之后仍然写得进去。
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

// TestAISessionSendWritesCallLog 发一条消息 → ai_call_log 落一条成功流水（带上游上报的用量）。
func TestAISessionSendWritesCallLog(t *testing.T) {
	ph, sess, svc, db := newSessionSendEnv(t)
	sessionSendUpstreamJSON(t, svc, http.StatusOK,
		`{"object":"response","output_text":"模型回复","usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`)
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(context.Background(), "send-log-1", "sess-page", "muse", "会话", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	rec := serveSessionSend(t, ph, "/admin/ai/sessions/send",
		"csrf_token=test-token&sessionId="+strconv.FormatInt(head.ID, 10)+"&providerKey=sess-page&model=muse&input=你好")
	assertAIJump(t, rec, true, "消息已发送，模型已回复")

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

// serveSessionsPage 走一次真实的会话页 GET（整页渲染，含权限集合）。
//
// 只补 user_id 与权限集合两件事：用例直挂 handler，不走 SessionAuth / CSRF / Casbin 那条链。
func serveSessionsPage(t *testing.T, ph *aihttp.SessionPageHandle) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("user_id", sessionSendUserID)
		c.Set(shell.PermSetKey, map[string]bool{
			"ai:provider_list": true, "ai:session_list": true, "ai:chat": true,
		})
		c.Set(shell.ButtonsKey, map[string]bool{
			"ai.provider_list": true, "ai.session_list": true, "ai.chat": true,
		})

		c.Next()
	})
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.GET("/admin/ai/sessions", ph.SessionsPage)

	req := httptest.NewRequest(http.MethodGet, "/admin/ai/sessions", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestAISessionSendPageShowsRecentCalls 发完消息再打开列表页 → 悬浮卡里出现这一次调用。
//
// 补的是「流水写了但看不见」这一段：写库与「读出来渲染」是两条链路，
// 只断言表里有行证明不了页面上有它 —— 而用户要的正是后者。
func TestAISessionSendPageShowsRecentCalls(t *testing.T) {
	ph, sess, svc, db := newSessionSendEnv(t)
	sessionSendUpstreamJSON(t, svc, http.StatusOK,
		`{"object":"response","output_text":"模型回复","usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`)
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(context.Background(), "send-log-3", "sess-page", "muse", "会话", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}
	rec := serveSessionSend(t, ph, "/admin/ai/sessions/send",
		"csrf_token=test-token&sessionId="+strconv.FormatInt(head.ID, 10)+"&providerKey=sess-page&model=muse&input=你好")
	assertAIJump(t, rec, true, "消息已发送，模型已回复")
	// 等流水落库之后再打开页面：异步写意味着「发完立刻查」可能还没写进去，
	// 而这里要验的是「页面上看得见」，不是「写得够快」。
	awaitCallLog(t, db, head.ID)

	page := serveSessionsPage(t, ph)
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

// TestAISessionSendWritesFailedCallLog 上游 5xx → 流水记一条 error（带归口后的 i18n key），
// 且只留 user 事件。
//
// 「只记成功的调用」在排查故障时等于没有日志：用户报「模型报错了」时最需要的恰恰是这一条。
// 同时钉住 error_key 的取值口径 —— 底层原文（上游报文）只进日志，不进表。
func TestAISessionSendWritesFailedCallLog(t *testing.T) {
	ph, sess, svc, db := newSessionSendEnv(t)
	sessionSendUpstreamJSON(t, svc, http.StatusInternalServerError, `{"error":"boom"}`)
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(context.Background(), "send-log-2", "sess-page", "muse", "会话", sessionSendUserID)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	rec := serveSessionSend(t, ph, "/admin/ai/sessions/send",
		"csrf_token=test-token&sessionId="+strconv.FormatInt(head.ID, 10)+"&providerKey=sess-page&model=muse&input=会失败")
	assertAIJump(t, rec, false, "")

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

// TestAISessionSendPageRedirectsErrOnEmptyInput 空输入 → 失败提示页，不落事件。
func TestAISessionSendPageRedirectsErrOnEmptyInput(t *testing.T) {
	ph, sess, svc, _ := newSessionSendEnv(t)
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(context.Background(), "send-key-2", "sess-page", "muse", "会话", 0)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	rec := serveSessionSend(t, ph, "/admin/ai/sessions/send",
		"csrf_token=test-token&sessionId="+strconv.FormatInt(head.ID, 10)+"&providerKey=sess-page&model=muse&input=")
	assertAIJump(t, rec, false, "请输入消息内容")
	if events, _, err := sess.ListEvents(context.Background(), head.ID, 1, 100); err != nil || len(events) != 0 {
		t.Fatalf("空输入不该落事件：n=%d err=%v", len(events), err)
	}
}

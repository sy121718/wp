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

	"strconv"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/templates"
)

// sessionSendBaseURL 全局单播的 documentation 网段字面量（只为过 SSRF 门禁）。
const sessionSendBaseURL = "http://198.51.100.7"

// newSessionSendEnv 真 PG + 真种子 + 真会话服务（已接上配置面的对话端口）。
func newSessionSendEnv(t *testing.T) (*aihttp.SessionPageHandle, *aiservice.SessionService, *aiservice.Service) {
	t.Helper()
	svc, db := newAIProviderService(t)
	sess := aiservice.NewSessionService(aimodel.NewSessionModel(db))
	sess.SetChatPort(svc)
	return aihttp.NewSessionPageHandle(sess, svc), sess, svc
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

// serveSessionSend 把请求交给会话页 handler（真实 Jet 引擎，只有 send 这一条写路由）。
func serveSessionSend(t *testing.T, ph *aihttp.SessionPageHandle, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
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
	ph, sess, svc := newSessionSendEnv(t)
	sessionSendUpstream(t, svc, "模型回复")
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(context.Background(), "send-key-1", "sess-page", "muse", "会话", 0)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	rec := serveSessionSend(t, ph, "/admin/ai/sessions/send",
		"csrf_token=test-token&sessionId="+strconv.FormatInt(head.ID, 10)+"&providerKey=sess-page&model=muse&input=你好")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("状态码 = %d，想要 303；body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/ai/sessions?") || !strings.Contains(loc, "id=") || !strings.Contains(loc, "done="+aienums.MsgSessionSent) {
		t.Fatalf("重定向地址不对：%s", loc)
	}

	events, _, err := sess.ListEvents(context.Background(), head.ID, 1, 100)
	if err != nil {
		t.Fatalf("读事件失败：%v", err)
	}
	if len(events) != 2 {
		t.Fatalf("事件条数 = %d，想要 2", len(events))
	}
}

// TestAISessionSendPageRedirectsErrOnEmptyInput 空输入 → 303，提示以 key 形式挂在 err 槽。
func TestAISessionSendPageRedirectsErrOnEmptyInput(t *testing.T) {
	ph, sess, svc := newSessionSendEnv(t)
	newSessionSendProvider(t, svc)

	head, err := sess.EnsureSession(context.Background(), "send-key-2", "sess-page", "muse", "会话", 0)
	if err != nil {
		t.Fatalf("建会话失败：%v", err)
	}

	rec := serveSessionSend(t, ph, "/admin/ai/sessions/send",
		"csrf_token=test-token&sessionId="+strconv.FormatInt(head.ID, 10)+"&providerKey=sess-page&model=muse&input=")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("状态码 = %d，想要 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "err="+aienums.ErrSessionChatInputRequired) {
		t.Fatalf("重定向地址没带 err key：%s", loc)
	}
	if events, _, err := sess.ListEvents(context.Background(), head.ID, 1, 100); err != nil || len(events) != 0 {
		t.Fatalf("空输入不该落事件：n=%d err=%v", len(events), err)
	}
}

// TestAISessionsPageRendersSendFormWithProviders 会话页在「有供应商」时渲出发消息区（真实 Jet 引擎）。
//
// 这条路径上最容易写错的是模板里两层 range（provider → 它的模型目录）与
// .Detail.ID 的取值：只有真渲一次才知道语法对不对 —— 错了就是运行时整页 500。
func TestAISessionsPageRendersSendFormWithProviders(t *testing.T) {
	svc, _ := newAIProviderService(t)
	newSessionSendProvider(t, svc)
	providers, err := svc.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("读供应商失败：%v", err)
	}
	if len(providers) == 0 {
		t.Fatal("供应商列表为空，用例前提不成立")
	}

	data := pageBaseData("AI 会话")
	data["Rows"] = []aidto.Session{}
	data["Total"] = 0
	data["Page"] = 1
	data["Keyword"] = ""
	data["Status"] = -1
	data["Detail"] = aidto.SessionDetail{Session: aidto.Session{ID: 7, SessionKey: "k-7", Title: "会话", Status: 1}}
	data["Events"] = []aidto.SessionEventItem{}
	data["Providers"] = providers

	body := renderAITemplate(t, "admin/ai/sessions", data)
	for _, want := range []string{
		`action="/admin/ai/sessions/send"`,
		`name="providerKey"`,
		`sess-page`,
		`name="sessionId" value="7"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("会话页发消息区缺少 %q", want)
		}
	}
	if strings.Contains(body, `{{`) {
		t.Fatal("渲染结果里残留未解析的模板标记")
	}
}

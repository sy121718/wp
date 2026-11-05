package aihttp

import (
	pageview "go_wp/pkg/view"
	"context"
	"errors"
	"go_wp/internal/adminui"
	json "go_wp/pkg/jsonx"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	aidto "go_wp/internal/module/ai/dto"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/templates"
	"go_wp/pkg/auth"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	aiSessionsRenderPermKey   = "perm_set"
	aiSessionsRenderButtonKey = "button_set"

	aiSessionsRenderSessionName = "ai-sessions-render-test"
	aiSessionsRenderCSRF        = "render-test-csrf-token"
)

var aiSessionsPageTestLogDirs sync.Map

type aiSessionsRenderStub struct {
	rows    []aidto.Session
	total   int64
	listErr error

	usageCalls   int
	trendCalls   int
	optionsCalls int
}

func (s *aiSessionsRenderStub) ListSessionsFiltered(context.Context, aidto.SessionQuery, int, int) ([]aidto.Session, int64, error) {
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	return s.rows, s.total, nil
}

func (s *aiSessionsRenderStub) SessionModelUsageOf(context.Context, []int64) (map[int64][]aidto.SessionModelUsage, error) {
	return map[int64][]aidto.SessionModelUsage{}, nil
}

func (s *aiSessionsRenderStub) SessionCallsOf(context.Context, []int64) (map[int64]aidto.SessionCalls, error) {
	return map[int64]aidto.SessionCalls{}, nil
}

func (s *aiSessionsRenderStub) SessionUsageOf(context.Context, aidto.SessionQuery) (aidto.SessionUsage, error) {
	s.usageCalls++
	return aidto.SessionUsage{}, nil
}

func (s *aiSessionsRenderStub) SessionTrend(context.Context, aidto.SessionQuery, int) (aidto.SessionTrend, error) {
	s.trendCalls++
	return aidto.SessionTrend{Series: []aidto.TrendSeries{}}, nil
}

func (s *aiSessionsRenderStub) SessionFilterOptions(context.Context) (aidto.SessionFilterOptions, error) {
	s.optionsCalls++
	return aidto.SessionFilterOptions{Providers: []string{}, Models: []string{}, Creators: []int64{}}, nil
}

func (s *aiSessionsRenderStub) GetSession(context.Context, int64) (*aidto.SessionDetail, error) {
	return nil, errors.New("aiSessionsRenderStub: 用例不加载详情")
}

func (s *aiSessionsRenderStub) ListEvents(context.Context, int64, int, int) ([]aidto.SessionEventItem, int64, error) {
	return nil, 0, errors.New("aiSessionsRenderStub: 用例不加载事件")
}

func (s *aiSessionsRenderStub) AppendEvent(context.Context, aidto.AppendEventReq) (*aidto.AppendEventResult, error) {
	return nil, errors.New("aiSessionsRenderStub: 用例不写事件")
}

func (s *aiSessionsRenderStub) RenameSession(context.Context, aidto.RenameSessionReq) (*aidto.Session, error) {
	return nil, errors.New("aiSessionsRenderStub: 用例不改名")
}

func (s *aiSessionsRenderStub) Fold(context.Context, aidto.FoldReq) (*aidto.FoldResult, error) {
	return nil, errors.New("aiSessionsRenderStub: 用例不折叠")
}

func (s *aiSessionsRenderStub) FoldPlan(context.Context, aidto.FoldPlanReq) (*aidto.FoldPlanResult, error) {
	return nil, errors.New("aiSessionsRenderStub: 用例不算折叠方案")
}

func (s *aiSessionsRenderStub) SendMessage(context.Context, aidto.SendMessageReq) (*aidto.SendMessageResult, error) {
	return nil, errors.New("aiSessionsRenderStub: 用例不发消息")
}

func (s *aiSessionsRenderStub) SendMessageStream(context.Context, aidto.SendMessageReq, func(aiservice.StreamEvent)) (*aidto.SendMessageResult, error) {
	return nil, errors.New("aiSessionsRenderStub: 用例不流式发消息")
}

func (s *aiSessionsRenderStub) RecentDialogue(context.Context, string, int, int64) ([]aidto.DialogueTurn, error) {
	return nil, errors.New("aiSessionsRenderStub: 用例不读最近对话")
}

type aiSessionsRenderProviders struct{}

func (aiSessionsRenderProviders) ListProviders(context.Context) ([]aidto.Provider, error) {
	return []aidto.Provider{}, nil
}

type aiSessionsRenderProvidersWithCard struct{ p aidto.Provider }

func (s aiSessionsRenderProvidersWithCard) ListProviders(context.Context) ([]aidto.Provider, error) {
	return []aidto.Provider{s.p}, nil
}

func newAISessionsPageTestRouter(t *testing.T, svc sessionServicePort) *gin.Engine {
	t.Helper()
	return newAISessionsPageTestRouterWithProviders(t, svc, aiSessionsRenderProviders{})
}

func newAISessionsPageTestRouterWithProviders(t *testing.T, svc sessionServicePort, providers providerLister) *gin.Engine {
	t.Helper()
	router, _ := newAISessionsPageTestRouterWithButtons(t, svc, providers, aiSessionsRenderDefaultButtons)
	return router
}

// aiSessionsRenderDefaultButtons 是绝大多数用例用的按钮集：卡片的三个写控件。
var aiSessionsRenderDefaultButtons = map[string]bool{
	"ai.provider_save": true, "ai.provider_status": true, "ai.provider_delete": true,
}

// newAISessionsPageTestRouterWithButtons 允许用例自己给按钮集 ——
// 「Buttons 透传」这条判据得能双向证伪：给了权限要出现，没给要消失。
func newAISessionsPageTestRouterWithButtons(t *testing.T, svc sessionServicePort, providers providerLister, buttons map[string]bool) (*gin.Engine, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	logDir := filepath.Join(t.TempDir(), "logs")
	aiSessionsPageTestLogDirs.Store(t.Name(), logDir)
	logConf := viper.New()
	logConf.Set("log.base_dir", logDir)
	if err := logger.Init(logConf); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = logger.Close() })
	if err := auth.Init(nil); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { t.Errorf("模板失败：%v", err) }))
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)

	store := cookie.NewStore([]byte("ai-sessions-page-render-test-secret-32b"))
	router.Use(sessions.Sessions(aiSessionsRenderSessionName, store))
	router.Use(func(c *gin.Context) {
		if s := sessions.Default(c); s != nil {
			s.Set("csrf_token", aiSessionsRenderCSRF)
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
		}

		c.Set(aiSessionsRenderPermKey, map[string]bool{"ai:session_list": true, "ai:provider_list": true})

		c.Set(aiSessionsRenderButtonKey, buttons)
		c.Next()
	})
	handle := NewSessionPageHandle(svc, providers)

	router.GET("/admin/ai/sessions", pageview.Slot(SessionsPageTemplate, adminui.RenderPage), handle.SessionsPage)
	return router, httptest.NewRecorder()
}

func requestAISessionsPage(t *testing.T, svc sessionServicePort) *httptest.ResponseRecorder {
	t.Helper()
	return requestAISessionsPageLang(t, svc, "")
}

func requestAISessionsPageLang(t *testing.T, svc sessionServicePort, lang string) *httptest.ResponseRecorder {
	t.Helper()
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	target := "/admin/ai/sessions"
	if lang != "" {
		target += "?lang=" + lang
	}
	router := newAISessionsPageTestRouter(t, svc)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func requestAISessionsPageWithProviders(t *testing.T, svc sessionServicePort, providers providerLister) *httptest.ResponseRecorder {
	t.Helper()
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	router, rec := newAISessionsPageTestRouterWithButtons(t, svc, providers, aiSessionsRenderDefaultButtons)
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/ai/sessions", nil))
	return rec
}

func TestAISessionsPageMainListFailureReturnsJSON(t *testing.T) {
	const rawErrText = `pq: relation "ai_sessions" does not exist (SQLSTATE 42P01)`

	stub := &aiSessionsRenderStub{listErr: errors.New(rawErrText)}
	rec := requestAISessionsPage(t, stub)
	body := rec.Body.String()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("主数据读不到应为 500，实际 %d：%s", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("失败出口应为 JSON，得到 %q", ct)
	}

	for _, banned := range []string{"</body>", "</html>", `id="adminRail"`, `class="tabs ai-tabs"`} {
		if strings.Contains(body, banned) {
			t.Errorf("失败出口不该渲染页壳（出现 %q）：%s", banned, body)
		}
	}
	if !strings.Contains(body, "系统内部错误，请稍后重试") {
		t.Errorf("失败响应应带归口文案：%s", body)
	}

	for _, forbidden := range []string{rawErrText, html.EscapeString(rawErrText), "ai.err.internal"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("失败响应不应出现 %q", forbidden)
		}
	}

	if stub.usageCalls != 0 || stub.trendCalls != 0 || stub.optionsCalls != 0 {
		t.Errorf("失败时不该再取看板/趋势/筛选候选，实际 usage=%d trend=%d options=%d",
			stub.usageCalls, stub.trendCalls, stub.optionsCalls)
	}
}

func TestAISessionsPageMainListFailureRendersEnglishWhenTermPresent(t *testing.T) {

	i18n.InjectForTest(map[string]map[string]string{
		"uiInternalErr": {"en-US": "Internal server error, please try again later"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	const rawErrText = `pq: relation "ai_sessions" does not exist (SQLSTATE 42P01)`
	router := newAISessionsPageTestRouter(t, &aiSessionsRenderStub{listErr: errors.New(rawErrText)})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/ai/sessions?lang=en-US", nil))
	body := rec.Body.String()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("主数据读不到应为 500，实际 %d：%s", rec.Code, body)
	}
	if !strings.Contains(body, "Internal server error, please try again later") {
		t.Errorf("英文请求应出现词条译文：%s", body)
	}

	for _, forbidden := range []string{"系统内部错误，请稍后重试", rawErrText} {
		if strings.Contains(body, forbidden) {
			t.Errorf("英文请求不应出现 %q", forbidden)
		}
	}
}

func TestAISessionsPageMainListFailureFallsBackWhenTermMissing(t *testing.T) {
	const rawErrText = `pq: relation "ai_sessions" does not exist (SQLSTATE 42P01)`
	rec := requestAISessionsPageLang(t, &aiSessionsRenderStub{listErr: errors.New(rawErrText)}, "en-US")
	body := rec.Body.String()

	if !strings.Contains(body, "系统内部错误，请稍后重试") {
		t.Errorf("缺词条时应落中文兜底：%s", body)
	}
	for _, forbidden := range []string{"ai.err.internal", rawErrText, html.EscapeString(rawErrText)} {
		if strings.Contains(body, forbidden) {
			t.Errorf("缺词条时不应出现 %q", forbidden)
		}
	}
}

func TestAISessionsPageSuccessEmptyRendersList(t *testing.T) {
	rec := requestAISessionsPage(t, &aiSessionsRenderStub{})
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("空列表应是 200，实际 %d：%s", rec.Code, body)
	}
	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "no-store") {
		t.Errorf("成功页不该带 no-store，得到 %q", cc)
	}
	for _, want := range []string{`class="tabs ai-tabs"`, "还没有任何会话。"} {
		if !strings.Contains(body, want) {
			t.Errorf("空态页应含 %q", want)
		}
	}
	for _, forbidden := range []string{"badge-danger", "这一页的数据没能读出来", "系统内部错误，请稍后重试"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("成功空态页不该出现失败痕迹 %q", forbidden)
		}
	}
}

func TestAISessionsPageSuccessWithRowRendersRow(t *testing.T) {
	stub := &aiSessionsRenderStub{
		rows: []aidto.Session{{
			ID: 7, SessionKey: "sess-7", Title: "会话渲染样例", ProviderKey: "openai",
			ModelID: "gpt-x", Status: 1, EventCount: 3, ContextTokens: 1200,
			UpdateTime: utils.NewJSONTime(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)),
		}},
		total: 1,
	}

	rec := requestAISessionsPage(t, stub)
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("有数据应是 200，实际 %d：%s", rec.Code, body)
	}
	for _, want := range []string{"会话渲染样例", `class="tabs ai-tabs"`} {
		if !strings.Contains(body, want) {
			t.Errorf("列表页应含 %q", want)
		}
	}
}

func TestAISessionsPageLoadFailureKeepsRawErrorInLog(t *testing.T) {
	rawErr := errors.New(`pq: relation "ai_sessions" does not exist (SQLSTATE 42P01)`)
	rec := requestAISessionsPage(t, &aiSessionsRenderStub{listErr: rawErr})

	body := rec.Body.String()
	for _, leaked := range []string{rawErr.Error(), html.EscapeString(rawErr.Error())} {
		if strings.Contains(body, leaked) {
			t.Fatalf("归口失败：原文进了页面：%s", body)
		}
	}

	if !aiSessionsTestLogHasRawError(t, rawErr) {
		t.Errorf("日志里没有 error=%q 的结构化记录 —— 归口变成了丢弃", rawErr.Error())
	}

	if aiSessionsTestLogHasRawError(t, errors.New(`pq: relation "never_happened_ai" does not exist`)) {
		t.Errorf("自检失败：本次没发生过的原文也被判定为命中")
	}
}

// TestAISessionsPageProviderCardExposesWriteControls 钉住「卡片透传 Buttons」。
//
// ProviderCardResp 曾经漏了 Buttons，cardData 也没把 base.Buttons 传下去 ——
// 卡片里每个 isset(.Buttons["ai.provider_save"]) 都是假，保存/启停/删除整片 hidden：
// 页面上看着像「这个账号没有任何权限」，而权限集其实齐着。
// 这里按真实渲染出来的 HTML 断言控件存在，不看 DTO 字段名。
func TestAISessionsPageProviderCardExposesWriteControls(t *testing.T) {
	providers := aiSessionsRenderProvidersWithCard{p: aidto.Provider{
		ID: 11, ProviderKey: "openai", DisplayName: "样例供应商", Protocol: "openai", Status: 1, Version: 3,
	}}
	rec := requestAISessionsPageWithProviders(t, &aiSessionsRenderStub{}, providers)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("有供应商应是 200，实际 %d：%s", rec.Code, body)
	}

	for _, want := range []string{
		`hx-post="/api/ai/provider/save"`,
		`hx-post="/api/ai/provider/status"`,
		`hx-post="/api/ai/provider/delete"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("卡片缺写控件 %q —— Buttons 没透传到卡片（权限集明明齐着）", want)
		}
	}

	// 反证：这份按钮集里没有 ai.provider_models_save，模型区的保存**按钮**必须不出现。
	// （表单元素本身是无条件渲染的骨架，靠按钮驱动提交；判据落在按钮上。）
	// 少了这条，「恒渲染」的实现也能让上面的断言通过。
	if strings.Contains(body, `class="btn btn-primary">保存模型目录`) {
		t.Error("按钮集里没有 ai.provider_models_save，模型区的保存按钮不该渲染")
	}
}

// TestAISessionsPageProviderModelsButtonsGateTheirControls 是上一条的反向验证：
// 把模型区权限加进按钮集之后，保存表单与候选按钮必须出现。
func TestAISessionsPageProviderModelsButtonsGateTheirControls(t *testing.T) {
	providers := aiSessionsRenderProvidersWithCard{p: aidto.Provider{
		ID: 12, ProviderKey: "openai", DisplayName: "样例供应商", Protocol: "openai", Status: 1, Version: 4,
	}}
	buttons := map[string]bool{
		"ai.provider_save": true, "ai.provider_status": true, "ai.provider_delete": true,
		"ai.provider_models_save": true, "ai.provider_models_candidates": true,
	}
	router, rec := newAISessionsPageTestRouterWithButtons(t, &aiSessionsRenderStub{}, providers, buttons)
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/ai/sessions", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("应是 200，实际 %d：%s", rec.Code, body)
	}

	for _, want := range []string{
		`hx-post="/api/ai/provider/models/save"`,
		`hx-post="/api/ai/provider/models/candidates"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("按钮集里有对应权限，页面应渲染 %q", want)
		}
	}
}

func TestAISessionsPageProviderCardBorrowsBaseKeys(t *testing.T) {
	providers := aiSessionsRenderProvidersWithCard{p: aidto.Provider{
		ID: 11, ProviderKey: "openai", DisplayName: "样例供应商", Protocol: "openai", Status: 1, Version: 3,
	}}
	rec := requestAISessionsPageWithProviders(t, &aiSessionsRenderStub{}, providers)
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("有供应商应是 200，实际 %d：%s", rec.Code, body)
	}

	// 保存表单是标准 HTMX 写表单：页面侧 /admin 写路由已随双轨一起清退。
	const cardForm = `hx-post="/api/ai/provider/save"`
	if !strings.Contains(body, "样例供应商") || !strings.Contains(body, cardForm) {
		t.Fatalf("供应商卡片没渲染出来（缺 %q 或卡片的保存表单）：%s", "样例供应商", body)
	}
	base := aiSessionsRenderBaseCSRF(t, body)
	if base == "" {
		t.Fatal("页面基座的 csrf_token 是空的 —— 会话里没有 token，本用例的断言无从成立")
	}
	values := aiSessionsRenderCSRFValues(body)
	if len(values) == 0 {
		t.Fatalf("页面里一个 csrf_token 隐藏域都没有：%s", body)
	}
	for _, v := range values {
		if v == "" {
			t.Errorf("存在空值的 csrf_token 隐藏域 —— 卡片借到的 csrf_token 是 nil")
		}
		if v != base {
			t.Errorf("隐藏域 csrf_token=%q 与页面基座值 %q 不一致", v, base)
		}
	}

	if got := aiSessionsRenderFirstCSRFValueAfter(body, cardForm); got != base {
		t.Errorf("卡片保存表单里的 csrf_token=%q，期望等于基座值 %q", got, base)
	}
}

func TestAISessionsPageFailureDoesNotRenderShell(t *testing.T) {
	rec := requestAISessionsPage(t, &aiSessionsRenderStub{listErr: errors.New("boom")})
	body := rec.Body.String()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("主数据失败应为 500，实际 %d：%s", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("失败出口应为 JSON，得到 %q", ct)
	}
	for _, banned := range []string{"csrf_token", "<form", "</body>", "</html>"} {
		if strings.Contains(body, banned) {
			t.Errorf("失败出口不该渲染页壳（出现 %q）：%s", banned, body)
		}
	}
}

func aiSessionsRenderBaseCSRF(t *testing.T, body string) string {
	t.Helper()
	const marker = `"X-CSRF-Token":"`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("页面里没有 hx-headers 的 X-CSRF-Token —— 基座没注入 csrf_token：%s", body)
	}
	rest := body[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatalf("hx-headers 的 X-CSRF-Token 值没有结束引号")
	}
	return rest[:j]
}

func aiSessionsRenderCSRFValues(body string) []string {
	const marker = `name="csrf_token" value="`
	var out []string
	rest := body
	for {
		i := strings.Index(rest, marker)
		if i < 0 {
			return out
		}
		rest = rest[i+len(marker):]
		j := strings.Index(rest, `"`)
		if j < 0 {
			return out
		}
		out = append(out, rest[:j])
		rest = rest[j:]
	}
}

func aiSessionsRenderFirstCSRFValueAfter(s, from string) string {
	const marker = `name="csrf_token" value="`
	start := 0
	if from != "" {
		i := strings.Index(s, from)
		if i < 0 {
			return ""
		}
		start = i
	}
	i := strings.Index(s[start:], marker)
	if i < 0 {
		return ""
	}
	rest := s[start+i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func aiSessionsTestLogDir(t *testing.T) string {
	t.Helper()
	value, ok := aiSessionsPageTestLogDirs.Load(t.Name())
	if !ok {
		t.Fatalf("用例 %s 尚未装配测试路由，日志目录未知", t.Name())
	}
	return value.(string)
}

func aiSessionsTestLogHasRawError(t *testing.T, rawErr error) bool {
	t.Helper()
	want := rawErr.Error()
	root := aiSessionsTestLogDir(t)
	found := false
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if !strings.Contains(filepath.ToSlash(path), "/ai/") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var record map[string]any
			if json.Unmarshal([]byte(line), &record) != nil {
				continue
			}
			if got, ok := record["error"].(string); ok && got == want {
				found = true
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("遍历日志目录 %s 失败：%v", root, err)
	}
	return found
}

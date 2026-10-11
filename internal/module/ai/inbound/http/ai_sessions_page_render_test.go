package aihttp

// ai_sessions_page_render_test.go — AI 会话页整页失败出口的回归护栏。
//
// 被守的边界（docs/rules/template-boundary.md §「整页 GET 的失败出口：失败也必须是『这一页』」，
// 规范样板 navigation_page.go:290-317）：
//
//  1. 主数据（会话清单 ListSessionsFiltered）读不到时仍是**这一页**：500 + text/html +
//     no-store + 完整页壳（此前走 shell.PageError → pkg/response 的 ErrorWithMessage，
//     浏览器拿到的是一段 JSON，页壳、侧栏、标题全丢）；
//  2. 失败优先空态：失败时不渲染标签区（指标卡 / 趋势 / 列表 / 供应商卡片）—— 那些块的
//     数据本次一条都没读到，零值渲染出来与「真的是 0 用量」长得一样；
//  3. 文案按请求语言归口（无词条 → 模块中文兜底），驱动原文只进日志，页面上不出原样与
//     HTML 转义两种形态，也不出裸 key；
//  4. 权限是事实不是渲染产物：失败页仍按真实权限集合计算 CanViewModels / CanViewSessions。
//
// 模板取自 templates/（与线上同一份），只有会话服务是替身 —— SessionPageHandle 的
// sessionServicePort 收窄成窄接口，就是为了让失败 / 空态 / 有数据这三档能在不连库时真跑一遍。

import (
	"context"
	"encoding/json"
	"errors"
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
	// 与 shell 的 context 键同值。这里写字符串常量而不引 shell 包标识符：
	// check-shell-migration.sh 的 test 桶基线只许下降，测试里每多一个 shell.* 都是一处使用点。
	aiSessionsRenderPermKey   = "perm_set"
	aiSessionsRenderButtonKey = "button_set"

	// 供应商卡片上的隐藏域借的是基座注入的 csrf_token，所以整页必须有**非空**的会话 CSRF：
	// 会话名与 token 值都写死，断言里拿它跟页面基座（layout 的 hx-headers）逐字比对。
	aiSessionsRenderSessionName = "ai-sessions-render-test"
	aiSessionsRenderCSRF        = "render-test-csrf-token"
)

// aiSessionsPageTestLogDirs 记录每个用例自己的日志目录（t.TempDir() 下）。
//
// logger 是**包级全局**配置（pkg/logger/manager.go:57-64 的 cfg/inited + 场景 logger 缓存）：
// 本包用例串行执行（全文件无 t.Parallel()），每次装配都把 logger.Init 指到本用例的目录，
// 断言因此只看得见本次请求写下的行 —— 共享目录下躺着历史运行的行，
// 「日志里找得到原文」这种断言在本次根本没写日志时也会通过。
var aiSessionsPageTestLogDirs sync.Map

// aiSessionsRenderStub 会话服务替身。
//
// 每个方法都写出来（而不是嵌入接口留 nil）：嵌入的 nil 接口一旦被调用就是 panic，
// 失败信息会指向替身自己，掩盖被测行为。用例会走到的只有列表 / 看板 / 筛选三组。
type aiSessionsRenderStub struct {
	rows    []aidto.Session
	total   int64
	listErr error

	// 读数据的调用计数：失败档要证明「成功路径的取数一行都没跑」—— 只看响应证明不了，
	// 一个顺手渲染了空看板的失败页在断言里与短路版长得一样。
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

// Series / Providers / Models / Creators 都给**非 nil 空切片**：模板对这几块用 len()，
// nil 会让渲染在那一行中断（现象是 HTTP 200 + 之后整块 HTML 消失，见 internal/templates/CLAUDE.md）。
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

func (s *aiSessionsRenderStub) RecentDialogue(context.Context, string, int) ([]aidto.DialogueTurn, error) {
	return nil, errors.New("aiSessionsRenderStub: 用例不读最近对话")
}

// aiSessionsRenderProviders 供应商替身：空列表（模型标签渲染自己的空态，不碰库）。
type aiSessionsRenderProviders struct{}

func (aiSessionsRenderProviders) ListProviders(context.Context) ([]aidto.Provider, error) {
	return []aidto.Provider{}, nil
}

// aiSessionsRenderProvidersWithCard 供应商替身：给一条记录 —— 整页就会 include
// provider_card.html（卡片是「借基座键」的那一侧，必须能被单独构造出来）。
type aiSessionsRenderProvidersWithCard struct{ p aidto.Provider }

func (s aiSessionsRenderProvidersWithCard) ListProviders(context.Context) ([]aidto.Provider, error) {
	return []aidto.Provider{s.p}, nil
}

// newAISessionsPageTestRouter 装配真实 Jet 渲染与真实页壳，只有会话服务是替身。
func newAISessionsPageTestRouter(t *testing.T, svc sessionServicePort) *gin.Engine {
	t.Helper()
	return newAISessionsPageTestRouterWithProviders(t, svc, aiSessionsRenderProviders{})
}

// newAISessionsPageTestRouterWithProviders 同上，但可换供应商替身。
func newAISessionsPageTestRouterWithProviders(t *testing.T, svc sessionServicePort, providers providerLister) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// 本用例独有的日志目录：断言按 t.Name() 取回（见 aiSessionsTestLogDir），
	// logger.Init 会清空场景 logger 缓存并按新配置重建，所以这里的目录确实生效。
	logDir := filepath.Join(t.TempDir(), "logs")
	aiSessionsPageTestLogDirs.Store(t.Name(), logDir)
	logConf := viper.New()
	logConf.Set("log.base_dir", logDir)
	if err := logger.Init(logConf); err != nil {
		t.Fatal(err)
	}
	// 收尾把全局 logger 关掉：否则句柄留在已删除的临时目录上，下一个用例若忘了 Init
	// 就会把行写进 gone 目录（读日志的断言于是变成「什么都没读到」）。
	t.Cleanup(func() { _ = logger.Close() })
	if err := auth.Init(nil); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	// 会话 + 预置 CSRF：adminui.Prepare 的 csrf_token 来自会话（builtin.GetCSRFToken），
	// 没有会话时它注入空串 —— 那样「卡片隐藏域非空」这条断言就永远测不到东西。
	store := cookie.NewStore([]byte("ai-sessions-page-render-test-secret-32b"))
	router.Use(sessions.Sessions(aiSessionsRenderSessionName, store))
	router.Use(func(c *gin.Context) {
		if s := sessions.Default(c); s != nil {
			s.Set("csrf_token", aiSessionsRenderCSRF)
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
		}
		// 权限是事实：失败页也要按真实集合算 CanView*，这里给两个读权限。
		c.Set(aiSessionsRenderPermKey, map[string]bool{"ai:session_list": true, "ai:provider_list": true})
		// 按钮码给全：卡片的保存表单在 {{if canSave}} 里，按钮码为空时那张表单（连带它的
		// csrf_token 隐藏域）根本不渲染 —— 断言会退化成「没东西可测」。
		c.Set(aiSessionsRenderButtonKey, map[string]bool{
			"ai.provider_save": true, "ai.provider_status": true, "ai.provider_delete": true,
		})
		c.Next()
	})
	handle := NewSessionPageHandle(svc, providers)
	router.GET("/admin/ai/sessions", handle.SessionsPage)
	return router
}

func requestAISessionsPage(t *testing.T, svc sessionServicePort) *httptest.ResponseRecorder {
	t.Helper()
	return requestAISessionsPageLang(t, svc, "")
}

// requestAISessionsPageLang 同上，但带 ?lang= 语言覆盖（pkg/response 的协商顺序：
// cookie → query lang → Accept-Language，测试用 query 这一档最直接）。
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

// requestAISessionsPageWithProviders 同上，但换一个供应商替身（整页会带上供应商卡片）。
func requestAISessionsPageWithProviders(t *testing.T, svc sessionServicePort, providers providerLister) *httptest.ResponseRecorder {
	t.Helper()
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	router := newAISessionsPageTestRouterWithProviders(t, svc, providers)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/ai/sessions", nil))
	return rec
}

// TestAISessionsPageMainListFailureRenders500 主数据读不到：同页完整 HTML 500 + no-store，
// 且成功路径的取数一行都不跑（短路，而不是「渲染一个空的成功页」）。
func TestAISessionsPageMainListFailureRenders500(t *testing.T) {
	const rawErrText = `pq: relation "ai_sessions" does not exist (SQLSTATE 42P01)`

	stub := &aiSessionsRenderStub{listErr: errors.New(rawErrText)}
	rec := requestAISessionsPage(t, stub)
	body := rec.Body.String()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("主数据读不到应为 500，实际 %d：%s", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type 应含 text/html，得到 %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("失败页应带 no-store，得到 %q", cc)
	}
	// 仍是这一页：完整页壳 + 侧栏 + 受控提示（失败说明优先于任何空态）
	for _, want := range []string{
		"</body>", "</html>", `id="adminRail"`,
		"服务器内部错误，请稍后重试",
		"这一页的数据没能读出来",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("失败页缺少 %q（页壳 / 受控提示）", want)
		}
	}
	// 原文（原样与 HTML 转义两形态）与裸 key 都不许出现；也不许把「没读出来」说成「还没有会话」
	for _, forbidden := range []string{
		rawErrText, html.EscapeString(rawErrText), "ai.err.internal",
		`class="tabs ai-tabs"`, "还没有任何会话。",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("失败页不应出现 %q", forbidden)
		}
	}
	// 短路证据：看板 / 趋势 / 筛选候选一次都没查（这些块本次一条数据都没有）
	if stub.usageCalls != 0 || stub.trendCalls != 0 || stub.optionsCalls != 0 {
		t.Errorf("失败时不该再取看板/趋势/筛选候选，实际 usage=%d trend=%d options=%d",
			stub.usageCalls, stub.trendCalls, stub.optionsCalls)
	}
}

// TestAISessionsPageMainListFailureRendersEnglishWhenTermPresent 词条在场时失败页说英文：
// 归口文案走的是「按请求语言取词」这条路，不是写死的中文。
func TestAISessionsPageMainListFailureRendersEnglishWhenTermPresent(t *testing.T) {
	// InjectForTest 的形状是 key → lang → 译文（与资源文件同构），不是 lang → key。
	i18n.InjectForTest(map[string]map[string]string{
		"ai.err.internal":                   {"en-US": "Internal server error, please try again later"},
		"admin.common.list.loadFailedTitle": {"en-US": "This page could not load its data"},
		"admin.common.list.loadFailedDesc":  {"en-US": "The tip at the top explains why, then retry in a moment."},
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
	for _, want := range []string{"Internal server error, please try again later", "This page could not load its data"} {
		if !strings.Contains(body, want) {
			t.Errorf("英文请求应出现词条译文 %q：%s", want, body)
		}
	}
	// 词条在场时不该再落中文兜底（兜底是缺失词条时的降级，不是常态）
	for _, forbidden := range []string{"服务器内部错误，请稍后重试", "这一页的数据没能读出来", rawErrText} {
		if strings.Contains(body, forbidden) {
			t.Errorf("英文请求不应出现 %q", forbidden)
		}
	}
}

// TestAISessionsPageMainListFailureFallsBackWhenTermMissing 缺词条（无 DB 的测试环境就是这个状态）：
// 落模块中文兜底，而不是裸 key、也不是原样把驱动错误摆上去。
func TestAISessionsPageMainListFailureFallsBackWhenTermMissing(t *testing.T) {
	const rawErrText = `pq: relation "ai_sessions" does not exist (SQLSTATE 42P01)`
	rec := requestAISessionsPageLang(t, &aiSessionsRenderStub{listErr: errors.New(rawErrText)}, "en-US")
	body := rec.Body.String()

	for _, want := range []string{"服务器内部错误，请稍后重试", "这一页的数据没能读出来"} {
		if !strings.Contains(body, want) {
			t.Errorf("缺词条时应落中文兜底 %q", want)
		}
	}
	for _, forbidden := range []string{"ai.err.internal", "admin.common.list.loadFailedTitle", rawErrText, html.EscapeString(rawErrText)} {
		if strings.Contains(body, forbidden) {
			t.Errorf("缺词条时不应出现 %q", forbidden)
		}
	}
}

// TestAISessionsPageSuccessEmptyRendersList 成功但为空：正常 200 页面 + 空态，不是失败页。
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
	for _, forbidden := range []string{"badge-danger", "这一页的数据没能读出来", "服务器内部错误，请稍后重试"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("成功空态页不该出现失败痕迹 %q", forbidden)
		}
	}
}

// TestAISessionsPageSuccessWithRowRendersRow 成功且有数据：标题出现在列表里。
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

// TestAISessionsPageLoadFailureKeepsRawErrorInLog 「归口」不是「丢弃」的正面证据。
//
// 上面的失败用例只证明了「原文没进页面」（反面证据）。这一条查另一端：内部错误必须能在
// **本次请求**写下的结构化日志里被完整找回 —— 否则 `aiErrKey` 注释许诺的「谁在哪个页面
// 撞上了哪条 SQL」就永远没人能回答（迁移 407 与 shell 的错误出口口径都要求留下可诊断性）。
func TestAISessionsPageLoadFailureKeepsRawErrorInLog(t *testing.T) {
	rawErr := errors.New(`pq: relation "ai_sessions" does not exist (SQLSTATE 42P01)`)
	rec := requestAISessionsPage(t, &aiSessionsRenderStub{listErr: rawErr})

	// 页面这一端：原样与 HTML 转义两种形态都不许出现（Jet 把 `"` 渲染成 &#34;）。
	body := rec.Body.String()
	for _, leaked := range []string{rawErr.Error(), html.EscapeString(rawErr.Error())} {
		if strings.Contains(body, leaked) {
			t.Fatalf("归口失败：原文进了页面：%s", body)
		}
	}
	// 日志这一端：ai scene 目录里必须有一条结构化记录的 error 字段**完整等于**原文。
	if !aiSessionsTestLogHasRawError(t, rawErr) {
		t.Errorf("日志里没有 error=%q 的结构化记录 —— 归口变成了丢弃", rawErr.Error())
	}
	// 自检：换一条本次没发生过的原文必须判为未命中 —— 否则「找得到」可能来自任何一行日志。
	if aiSessionsTestLogHasRawError(t, errors.New(`pq: relation "never_happened_ai" does not exist`)) {
		t.Errorf("自检失败：本次没发生过的原文也被判定为命中")
	}
}

// TestAISessionsPageProviderCardBorrowsBaseKeys 供应商卡片借的是基座的键。
//
// provider_card.html 是 include 进来的独立作用域，它的取词函数 / csrf_token / PermSet 全部
// 来自 cardData(base, …) 的 "t": base["t"] / "csrf_token": base["csrf_token"] /
// "PermSet": base["PermSet"]（ai_page.go 的 cardData）。所以 Prepare 必须发生在
// attachProviderCards **之前** —— 挪到末尾时卡片拿到三个 nil：取词当场渲染失败（整页 500，
// 已被 feature 用例 TestAISessionSendPageShowsRecentCalls 抓住），而 csrf_token 为 nil 只会
// 静默渲染成空隐藏域（页面 200、表单看着正常，提交时才被 CSRF 拒）。
//
// 判据因此不看状态码，只钉「卡片自己的隐藏域非空且等于页面基座值」。
func TestAISessionsPageProviderCardBorrowsBaseKeys(t *testing.T) {
	providers := aiSessionsRenderProvidersWithCard{p: aidto.Provider{
		ID: 11, ProviderKey: "openai", DisplayName: "样例供应商", Protocol: "openai", Status: 1, Version: 3,
	}}
	rec := requestAISessionsPageWithProviders(t, &aiSessionsRenderStub{}, providers)
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("有供应商应是 200，实际 %d：%s", rec.Code, body)
	}
	// 先证明卡片真的渲染了：否则下面的断言会因为「没有卡片」而空转通过。
	const cardForm = `action="/admin/ai/providers/save"`
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
	// 卡片自己的那一个：定位到卡片的保存表单之后取，证明它不是「恰好基座那一处」。
	if got := aiSessionsRenderFirstCSRFValueAfter(body, cardForm); got != base {
		t.Errorf("卡片保存表单里的 csrf_token=%q，期望等于基座值 %q", got, base)
	}
}

// TestAISessionsPageFailurePageAlsoCarriesCSRF 失败档同样带非空 CSRF。
//
// Prepare 在函数开头调用一次、两个分支共用：失败页上仍有侧栏与页面级表单，
// 它的 csrf_token 不该因为「主数据没读出来」而变空。
func TestAISessionsPageFailurePageAlsoCarriesCSRF(t *testing.T) {
	rec := requestAISessionsPage(t, &aiSessionsRenderStub{listErr: errors.New("boom")})
	body := rec.Body.String()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("主数据失败应为 500，实际 %d：%s", rec.Code, body)
	}
	if base := aiSessionsRenderBaseCSRF(t, body); base == "" {
		t.Error("失败页的基座 csrf_token 为空（Prepare 没在这条分支生效）")
	}
	for _, v := range aiSessionsRenderCSRFValues(body) {
		if v == "" {
			t.Error("失败页出现空值的 csrf_token 隐藏域")
		}
	}
}

// aiSessionsRenderBaseCSRF 取页面基座（layout 的 hx-headers）里的 CSRF token。
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

// aiSessionsRenderCSRFValues 取出全页 name="csrf_token" value="…" 的所有值。
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

// aiSessionsRenderFirstCSRFValueAfter 在 s[from:] 里找第一个 csrf_token 隐藏域的值；
// 找不到（或 from 不在 s 里）返回空串。
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

// aiSessionsTestLogDir 取回本用例的日志目录；取不到说明断言落在没装配过路由的用例里。
func aiSessionsTestLogDir(t *testing.T) string {
	t.Helper()
	value, ok := aiSessionsPageTestLogDirs.Load(t.Name())
	if !ok {
		t.Fatalf("用例 %s 尚未装配测试路由，日志目录未知", t.Name())
	}
	return value.(string)
}

// aiSessionsTestLogHasRawError 在本用例专属日志目录里逐行解析结构化记录，判断有没有一条把原始
// 错误**完整**留在 error 字段里。
//
// 三个刻意的选择：
//
//	· 解析 JSON 后比对 error 字段的完整值，而不是在整段文本里找子串 —— 子串会把「转义之后照样
//	  看得见」（`\"` → `"`）也算命中，那是另一种形式的没写进去；
//	· 目录来自 t.TempDir()（见 aiSessionsPageTestLogDirs），本用例之外的行进不来；
//	· 只认 ai scene 子目录：这条错误是 AI 页写下的（生产侧 `logger.Scene("ai")`，见 ai_err.go:34），
//	  落在别的 scene 里说明记录位置漂移了。
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

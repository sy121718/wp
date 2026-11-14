package projecthttp

import (
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

const settingsDocument = `{"settings":{"layout":{"mode":"boxed"},"seo":{"title":"标题A","description":"描述B","focusKeyword":"kw","schemaType":"article","secondaryKeywords":["a","b"],"intent":"commercial"}},"root":[]}`

func newSettingsRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)

	router.POST("/workbench/settings", pageview.Slot(SettingsPanelFragmentTemplate, pageview.RenderBare), workbenchSettingsPanel)
	router.POST("/workbench/seo-score-panel", pageview.Slot(SeoScoreFragmentTemplate, pageview.RenderBare), workbenchSeoScorePanel)
	return router
}

// panelJSON 构造面板的明确 JSON 输入：面板是复杂编辑器输入，协议固定为 JSON。
func panelJSON(t *testing.T, router *gin.Engine, path, body string) (int, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	return recorder.Code, recorder.Body.String()
}

func fetchSettings(t *testing.T, router *gin.Engine, path string) string {
	t.Helper()
	code, body := panelJSON(t, router, path, `{"document":`+settingsDocument+`,"url":"/about"}`)
	if code != http.StatusOK {
		t.Fatalf("状态码 %d", code)
	}
	return body
}

func TestSettingsPanelRendersForm(t *testing.T) {
	body := fetchSettings(t, newSettingsRouter(t), "/workbench/settings")
	for _, want := range []string{
		`data-wb-setting="settings.layout.mode" data-wb-value="boxed"`,
		`data-wb-setting="settings.seo.title" value="标题A"`,
		`data-wb-setting="settings.seo.description"`,
		`data-wb-setting="settings.seo.schemaType" data-wb-value="article"`,
		`data-wb-kind="list" value="a b"`,
		`data-wb-setting="settings.seo.intent" data-wb-value="commercial"`,
		`id="wb-seo-score"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("设置面板缺少 %q\n%s", want, body)
		}
	}

	if !strings.Contains(body, `class="wb-seg-btn is-active" data-wb-setting="settings.layout.mode" data-wb-value="boxed"`) {
		t.Errorf("版心模式选中态未渲染\n%s", body)
	}
}

func TestSeoScorePanelRendersScore(t *testing.T) {
	body := fetchSettings(t, newSettingsRouter(t), "/workbench/seo-score-panel")
	for _, want := range []string{
		`wb-seo-head`, `wb-seo-total`, `wb-seo-grade`,
		`wb-seo-dot is-`, `wb-seo-serp`, `wb-serp-title`, `标题A`, `wb-serp-url`, `/about`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("评分区缺少 %q\n%s", want, body)
		}
	}
}

// 三块工作台面板的守卫本身：鉴权器不可用时必须拒绝，不能返回 200。
//
// 注意范围：这里手工写「守卫 + 渲染 + handler」，只验证守卫**行为**；
// 生产注册处是否真的挂了守卫，由 public/test/workbench/feature 的
// TestWorkbenchPanelGuardComesFromProductionRouter 直接调用 SetupProjectPages 证明。
// 请求带头上的 test_user，走到策略判定而不是「未登录」分支。
func TestWorkbenchPanelsRejectWhenAuthorizerUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// 身份必须在**注册路由之前**装上：Gin 的路由在注册时快照中间件链，
	// 之后再 Use 不会补到已有路由上（那样拒绝就退回到「未登录」，测不到策略层）。
	router.Use(func(c *gin.Context) { c.Set("user_id", int64(810)); c.Next() })
	pages := router.Group("")

	// 与生产注册同形：每条都是「鉴权守卫 + 渲染接线 + handler」。
	panels := []struct {
		path    string
		tpl     string
		handler gin.HandlerFunc
	}{
		{"/workbench/settings", SettingsPanelFragmentTemplate, workbenchSettingsPanel},
		{"/workbench/seo-score-panel", SeoScoreFragmentTemplate, workbenchSeoScorePanel},
		{"/workbench/global", GlobalPanelFragmentTemplate, workbenchGlobalPanel},
	}
	for _, p := range panels {
		pages.POST(p.path, builtin.CasbinMiddlewareForPath("/api/project/update"),
			pageview.Slot(p.tpl, pageview.RenderBare), p.handler)
	}

	// 有身份、但 Casbin 未初始化：拒绝必须来自鉴权器不可用，而不是未登录。
	for _, tc := range panels {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{"document":{}}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Errorf("%s 在鉴权不可用时返回 200 —— 说明这条入口没有实际守卫", tc.path)
		}
	}
}

// 面板输入协议固定为 JSON：表单编码不能再被悄悄接受。
//
// 否则「输入协议」只是文档里的一句话 —— 同一端点仍能收两种形状，
// 契约与门禁就都失去了权威来源。
func TestWorkbenchPanelsRejectFormEncodedInput(t *testing.T) {
	for _, tc := range []struct {
		path string
		tpl  string
		h    gin.HandlerFunc
	}{
		{"/workbench/settings", SettingsPanelFragmentTemplate, workbenchSettingsPanel},
		{"/workbench/seo-score-panel", SeoScoreFragmentTemplate, workbenchSeoScorePanel},
		{"/workbench/global", GlobalPanelFragmentTemplate, workbenchGlobalPanel},
	} {
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
		// 这里只验「绑定失败 → 400」，不验错误页模板；用最小出口把状态码原样写出，
		// 避免把模板取词失败误当成入口行为。
		router.Use(pageview.FailureMiddleware(func(c *gin.Context, status int, _ string, _ error) {
			c.Status(status)
		}))
		router.POST(tc.path, pageview.HTMLFragment, pageview.Slot(tc.tpl, pageview.RenderBare), tc.h)

		form := url.Values{"document": {settingsDocument}, "url": {"/about"}, "themeId": {"t-1"}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s 接受表单编码（状态码 %d），输入协议必须唯一为 JSON", tc.path, rec.Code)
		}
	}
}

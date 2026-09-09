package feature

// settings_panel_test.go — 页面设置面板与评分区的服务端渲染（HTMX 化，docs/09 §3）。
//
// 验证：表单字段带 data-wb-setting、segment 选中态、次级关键词空格连接、
// 评分区（总分/等级/圆点/SERP 预览）由服务端渲染。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

// settingsDocument 带完整 SEO 设置的草稿文档。
const settingsDocument = `{"settings":{"layout":{"mode":"boxed"},"seo":{"title":"标题A","description":"描述B","focusKeyword":"kw","schemaType":"article","secondaryKeywords":["a","b"],"intent":"commercial"}},"root":[]}`

// newSettingsRouter 装配设置面板相关端点的测试路由。
func newSettingsRouter(t *testing.T) *gin.Engine {
	t.Helper()
	handle := dashboardhttp.NewHandle(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.POST("/workbench/settings", handle.SettingsPanel)
	router.POST("/workbench/seo-score-panel", handle.SeoScorePanel)
	return router
}

// fetchSettings 请求设置面板片段。
func fetchSettings(t *testing.T, router *gin.Engine, path string) string {
	t.Helper()
	form := url.Values{"document": {settingsDocument}, "url": {"/about"}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 %d", recorder.Code)
	}
	return recorder.Body.String()
}

// TestSettingsPanelRendersForm 表单字段、segment 选中态与评分容器渲染完整。
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
	// 定宽模式选中（属性顺序：class 在 data 之前）。
	if !strings.Contains(body, `class="wb-seg-btn is-active" data-wb-setting="settings.layout.mode" data-wb-value="boxed"`) {
		t.Errorf("版心模式选中态未渲染\n%s", body)
	}
}

// TestSeoScorePanelRendersScore 评分区由服务端渲染（总分/等级/圆点/SERP 预览）。
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

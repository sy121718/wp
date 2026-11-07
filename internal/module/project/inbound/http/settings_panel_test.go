package projecthttp

import (
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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

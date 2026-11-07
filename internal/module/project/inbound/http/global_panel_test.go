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

const globalPanelThemeSettingsJSON = `{"colors":{"primary":"#111111"},"typography":{"heading":{"fontWeight":"600"}},"button":{"paddingY":"12px"}}`

func newGlobalRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)

	router.POST("/workbench/global", pageview.Slot(GlobalPanelFragmentTemplate, pageview.RenderBare), workbenchGlobalPanel)
	return router
}

func TestGlobalPanelRendersFields(t *testing.T) {
	form := url.Values{"themeId": {"theme-1"}, "settings": {globalPanelThemeSettingsJSON}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/global", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	newGlobalRouter(t).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`wb-palette-group">颜色<`,
		`wb-palette-group">按钮<`,
		`data-wb-theme-color="colors.primary" data-wb-theme-path="colors.primary"`,
		`name="typography.heading.weight"`,
		`<option value="600" selected>半粗</option>`,
		`<select name="button.py"`,
		`<option value="12px" selected>12px</option>`,

		`<select name="typography.heading.size"`,
		`<select name="typography.body.line"`,
		`<select name="surface.radius"`,
		`<select name="motion.duration"`,
		`<select name="button.borderStyle"`,

		`list="theme-dl-typography.heading.font"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("全局设置面板缺少 %q\n%s", want, body)
		}
	}
}

func TestGlobalPanelEmptyState(t *testing.T) {
	form := url.Values{"themeId": {""}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/global", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	newGlobalRouter(t).ServeHTTP(recorder, req)
	if !strings.Contains(recorder.Body.String(), "未挂接主题") {
		t.Errorf("应输出空态提示，实际: %s", recorder.Body.String())
	}
}

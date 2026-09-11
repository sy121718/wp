package feature

// global_panel_test.go — 全局设置（站点主题）面板服务端渲染（HTMX 化，docs/09 §3）。
//
// 验证：字段表由服务端渲染（分组标题/颜色槽/select 选中态/文本值）、
// 未挂主题时输出空态提示。

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

// themeSettingsJSON 主题设置样例（回显路径与提交键名不同，用于验证映射）。
const themeSettingsJSON = `{"colors":{"primary":"#111111"},"typography":{"heading":{"fontWeight":"600"}},"button":{"paddingY":"12px"}}`

// newGlobalRouter 装配全局设置端点的测试路由。
func newGlobalRouter(t *testing.T) *gin.Engine {
	t.Helper()
	handle := dashboardhttp.NewHandle(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.POST("/workbench/global", handle.GlobalPanel)
	return router
}

// TestGlobalPanelRendersFields 字段表与当前值渲染完整。
func TestGlobalPanelRendersFields(t *testing.T) {
	form := url.Values{"themeId": {"theme-1"}, "settings": {themeSettingsJSON}}
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
		// 固定档位一律下拉：数字与枚举让用户手填是「不会用」的主要来源。
		`<select name="typography.heading.size"`,
		`<select name="typography.body.line"`,
		`<select name="surface.radius"`,
		`<select name="motion.duration"`,
		`<select name="button.borderStyle"`,
		// 字体可填可选（datalist），不锁死成固定几款。
		`list="theme-dl-typography.heading.font"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("全局设置面板缺少 %q\n%s", want, body)
		}
	}
}

// TestGlobalPanelEmptyState 未挂主题时输出空态提示。
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

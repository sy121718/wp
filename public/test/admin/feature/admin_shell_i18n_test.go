package feature

import (
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	workbenchhttp "go_wp/internal/module/workbench/inbound/http"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

func newAdminShellEngine(t *testing.T) *gin.Engine {
	t.Helper()

	home := &workbenchhttp.Handle{}
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath:     support.NewComponentTestConfig(t),
		GinMode:        gin.TestMode,
		InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {

			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			e.GET("/admin", pageview.Slot(workbenchhttp.DashboardPageTemplate, adminui.RenderPage), home.Dashboard)
		},
	})
	if err != nil {
		t.Fatalf("隔离依赖已就绪，组件初始化失败: %v", err)
	}
	t.Cleanup(func() { _ = cleanup() })
	return engine
}

func fetchAdmin(t *testing.T, engine *gin.Engine, lang string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	if lang != "" {
		req.Header.Set("Cookie", "lang="+lang)
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /admin (lang=%s) 状态码 %d: %s", lang, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func TestAdminShellI18nSwitchesWithCookie(t *testing.T) {
	engine := newAdminShellEngine(t)

	zh := fetchAdmin(t, engine, "zh-CN")
	for _, want := range []string{
		"<html lang=\"zh-CN\">",
		"管理后台", "切换主题", "关闭",
		"action=\"/admin/lang\"", "value=\"en-US\"", "value=\"zh-CN\"",
	} {
		if !strings.Contains(zh, want) {
			t.Fatalf("zh 页面缺少 %q", want)
		}
	}

	en := fetchAdmin(t, engine, "en-US")
	for _, want := range []string{
		"<html lang=\"en-US\">",
		"Admin Console", "Toggle theme", "Close",
		"action=\"/admin/lang\"", "value=\"en-US\"",
	} {
		if !strings.Contains(en, want) {
			t.Fatalf("en 页面缺少 %q", want)
		}
	}

	for _, unwanted := range []string{"切换主题", "管理后台"} {
		if strings.Contains(en, unwanted) {
			t.Fatalf("en 页面不应再出现中文 %q", unwanted)
		}
	}
	if zh == en {
		t.Fatal("切换语言后页面字节完全相同，说明文案未随语言变化")
	}

	ja := fetchAdmin(t, engine, "ja-JP")
	if !strings.Contains(zh, "仪表盘") || !strings.Contains(ja, "仪表盘") {
		t.Fatal("zh / 未知语言(ja-JP) 的标题应回退到 zh-CN 译文「仪表盘」")
	}
	if !strings.Contains(en, "Dashboard") {
		t.Fatal("en 页面标题应命中 en-US 词条 Dashboard")
	}
	for _, body := range []string{zh, en, ja} {
		if strings.Contains(body, "MsgDashboardTitle") {
			t.Fatal("标题不应显示裸 key（应命中 i18n 词条）")
		}
	}
}

func TestAdminShellI18nNoRawKeys(t *testing.T) {
	engine := newAdminShellEngine(t)

	body := fetchAdmin(t, engine, "zh-CN")
	for _, key := range []string{"shell.brand", "shell.action.close", "shell.action.toggle_theme", "shell.lang.label"} {
		if strings.Contains(body, key) {
			t.Fatalf("页面不应输出裸 key %q", key)
		}
	}

	fallback := fetchAdmin(t, engine, "ja-JP")
	if !strings.Contains(fallback, "管理后台") {
		t.Fatal("未知语言应回退默认语言（zh-CN）文案")
	}
}

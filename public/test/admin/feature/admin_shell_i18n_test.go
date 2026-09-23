package feature

// admin_shell_i18n_test.go — 后台外壳多语言（多语言 P1 第二步）接口级验证。
//
// 走真实链路：gin 路由 → dashboard handler → withI18n → Jet 模板 → pkg/i18n 缓存（DB 词条）。
// 断言同一路径在 lang=zh-CN / lang=en-US 下输出不同文案（HTML 的 lang 属性同步变化），
// 并覆盖缺词条回退：标题 MsgDashboardTitle 无 en-US 词条时回退 zh-CN 译文，不显示裸 key。
//
// PG/Redis 不可用时 t.Skip（与其他功能测试一致）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workbenchhttp "go_wp/internal/module/workbench/inbound/http"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

// newAdminShellEngine 装配后台页面外壳（GET /admin，仪表盘 handler，无需业务服务）。
func newAdminShellEngine(t *testing.T) *gin.Engine {
	t.Helper()

	home := &workbenchhttp.Handle{}
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath:     support.NewComponentTestConfig(t),
		GinMode:        gin.TestMode,
		InitComponents: true, // 初始化 database/redis/i18n，让模板层读到真实 sys_i18n 词条
		RouteRegistrar: func(e *gin.Engine) {
			// 默认路由未装配时需自行挂 Jet 渲染器（生产由 routers.SetupRoutes 装配）。
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			e.GET("/admin", home.Dashboard)
		},
	})
	if err != nil {
		t.Fatalf("隔离依赖已就绪，组件初始化失败: %v", err)
	}
	t.Cleanup(func() { _ = cleanup() })
	return engine
}

// fetchAdmin 请求 /admin 并带上语言 Cookie（走 pkg/response 的语言协商第 1 优先级）。
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

// TestAdminShellI18nSwitchesWithCookie 同一页面随 Cookie 语言切换文案。
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
	// 关键差异：同一位置的中文原文在 en 页面不应出现（已命中 en-US 词条）
	for _, unwanted := range []string{"切换主题", "管理后台"} {
		if strings.Contains(en, unwanted) {
			t.Fatalf("en 页面不应再出现中文 %q", unwanted)
		}
	}
	if zh == en {
		t.Fatal("切换语言后页面字节完全相同，说明文案未随语言变化")
	}

	// 标题：MsgDashboardTitle 在迁移 430 之前**只有 zh-CN 词条**，本条断言当时拿「缺 en-US」
	// 当 fallback 的夹具。430 按「值即 key、库里只有 zh-CN」的口径补齐了它的 en-US ——
	// 夹具改为**未知语言**（ja-JP）：它没有任何词条、协商回退默认语言（zh-CN），
	// 覆盖的是同一条链路（回退到 zh-CN 译文 + 不显示裸 key），且不再依赖
	// 「某个 key 恰好缺 en-US」这个会随补词条批次失效的前提。
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

// TestAdminShellI18nNoRawKeys 页面不输出裸 key（缺词条走模板内中文原文兜底）。
func TestAdminShellI18nNoRawKeys(t *testing.T) {
	engine := newAdminShellEngine(t)

	body := fetchAdmin(t, engine, "zh-CN")
	for _, key := range []string{"shell.brand", "shell.action.close", "shell.action.toggle_theme", "shell.lang.label"} {
		if strings.Contains(body, key) {
			t.Fatalf("页面不应输出裸 key %q", key)
		}
	}
	// 未知语言（协商回退默认语言）同样渲染中文原文，不报错
	fallback := fetchAdmin(t, engine, "ja-JP")
	if !strings.Contains(fallback, "管理后台") {
		t.Fatal("未知语言应回退默认语言（zh-CN）文案")
	}
}

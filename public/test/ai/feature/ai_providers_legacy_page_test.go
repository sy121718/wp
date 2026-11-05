/* ai_providers_legacy_page_test.go — 老地址 /admin/ai/providers 的真实出口。
 *
 * 背景：532 迁移把「AI 模型」菜单并进了 /admin/ai/sessions?tab=models，
 * 534 又把插回来的旧菜单行软删。老地址留着只为旧书签不 404。
 *
 * 它曾经发 302 跳到新地址。按 AGENTS「没有后端的成功重定向」，页面 GET 成功
 * 必须是「模板 + 业务 data」，不是重定向。这个用例把这条协议钉住：
 *
 *   1. 状态 200、正文是整页 HTML（有 <html 与页面标题），不是 302；
 *   2. 落在「模型」标签上（与从菜单进来一致），不是会话标签；
 *   3. 语言跟着请求走：中文出中文标题，英文出英文标题，且都不含裸 key。
 */
package feature

import (
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aimodel "go_wp/internal/module/ai/model"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/templates"
	text "go_wp/pkg/view/jet"
)

// newLegacyProvidersEngine 挂上真实的会话页 handle —— 老地址复用同一个渲染函数。
func newLegacyProvidersEngine(t *testing.T) *gin.Engine {
	t.Helper()
	svc, db := newAIProviderService(t)
	sess := aiservice.NewSessionService(aimodel.NewSessionModel(db))
	sessionPage := aihttp.NewSessionPageHandle(sess, svc)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	// 页面渲染要语言与取词函数；生产上由 assembly 的 AttachScope 中间件提供。
	engine.Use(func(c *gin.Context) {
		// 生产链上这两样都由中间件提供：语言/取词作用域，与权限位集合。
		text.AttachScope(c)
		c.Set(adminctx.PermSetKey, map[string]bool{
			"ai:provider_list": true,
			"ai:session_list":  true,
		})
		c.Next()
	})
	engine.GET("/admin/ai/providers", pageview.Slot(aihttp.SessionsPageTemplate, adminui.RenderPage), sessionPage.ProvidersPage)
	engine.GET("/admin/ai/sessions", pageview.Slot(aihttp.SessionsPageTemplate, adminui.RenderPage), sessionPage.SessionsPage)
	return engine
}

func TestLegacyProvidersPathRendersModelsTabNotRedirect(t *testing.T) {
	engine := newLegacyProvidersEngine(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/ai/providers", nil)
	req.Header.Set("Accept-Language", "zh-CN")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("老地址应直接渲染页面，实际状态 %d（重定向不是页面 GET 的合法出口）", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("页面 GET 不得发重定向，却带了 Location=%q", loc)
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.Contains(ctype, "text/html") {
		t.Fatalf("页面 GET 成功应回模板渲染结果，实际 Content-Type=%q", ctype)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "<html") {
		t.Fatalf("应是整页 HTML，正文前 200 字：%q", firstN(body, 200))
	}
	if strings.Contains(body, "data-jump-state") {
		t.Error("不应出现跳转页标记：页面 GET 没有中间跳转")
	}

	// 页面标题来自模板 pageTitle block（模型页的 UI key），不是裸 key。
	if strings.Contains(body, "admin.ai.") {
		t.Error("正文不应出现裸 i18n key")
	}

	// 落在模型标签：模型区（含空态）应在，会话区的筛选条不该在。
	if !strings.Contains(body, `id="ai-panel-models"`) || !strings.Contains(body, "还没有配置任何 AI 供应商。") {
		t.Error("老地址应落在模型标签，页面上却没渲染出模型目录区")
	}
	if strings.Contains(body, "/admin/ai/sessions?") && !strings.Contains(body, "tab=models") {
		t.Error("老地址生效时应是模型标签，不该出现会话标签的筛选链接")
	}
}

func TestLegacyProvidersPathHonoursRequestLanguage(t *testing.T) {
	engine := newLegacyProvidersEngine(t)

	zh := httptest.NewRecorder()
	zhReq := httptest.NewRequest(http.MethodGet, "/admin/ai/providers", nil)
	zhReq.Header.Set("Accept-Language", "zh-CN")
	engine.ServeHTTP(zh, zhReq)

	en := httptest.NewRecorder()
	enReq := httptest.NewRequest(http.MethodGet, "/admin/ai/providers", nil)
	enReq.Header.Set("Accept-Language", "en-US")
	enReq.AddCookie(&http.Cookie{Name: "lang", Value: "en-US"})
	engine.ServeHTTP(en, enReq)

	zhBody, enBody := zh.Body.String(), en.Body.String()
	if zhBody == enBody {
		t.Fatal("两种语言渲染出完全相同的正文，说明语言没有按请求生效")
	}
	// 用页里的取词结果比对：中文正文里应出现中文界面词，英文正文里不该残留同一句。
	word := "恢复默认模型" // admin.ai.models.restore 的中文兜底
	if !strings.Contains(zhBody, word) && !strings.Contains(zhBody, "大模型管理") {
		t.Error("中文请求的页面里应出现中文界面词")
	}
	if strings.Contains(enBody, "大模型管理") {
		t.Error("英文请求的页面里不应残留中文成品句")
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

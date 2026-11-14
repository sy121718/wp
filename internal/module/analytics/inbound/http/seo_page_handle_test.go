package analyticshttp

import (
	"context"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	analyticscontract "go_wp/internal/module/analytics/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

type fakeSEOAnalytics struct {
	analyticscontract.AnalyticsService
	res *analyticscontract.SummaryResp
}

func (f fakeSEOAnalytics) Summary(_ context.Context, _ *analyticscontract.SummaryReq) (*analyticscontract.SummaryResp, error) {
	return f.res, nil
}

func TestSEOPageTemplateRendersAvailableAndMissingData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lang := "zh-CN"
	data := gin.H{
		"menu":          "seo",
		"csrf_token":    "test-token",
		"lang":          lang,
		"t":             templates.TranslateFunc(lang),
		"langs":         templates.LanguageOptions(lang),
		"lang_redirect": "/admin/seo",

		"Projects": []projectcontract.ProjectResp{
			{ID: "p-1", Name: "演示站"}, {ID: "p-2", Name: "备用站"},
		},
		"SelectedProject": "p-1",
		"Paths": []analyticscontract.PathCount{
			{Path: "/guides/seo", Views: 42, Visitors: 17},
		},
		"HasPaths": true,
		"Buttons":  map[string]bool{"seo.audit": true},
	}
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/seo", pageview.Slot(SEOPageTemplate, pageview.RenderPage), func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/analytics/seo.html", data)
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/seo", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"SEO 控制台", "演示站", "/guides/seo", "42", "17",
		"data-seo-audit-action", "data-seo-audit-result",
		// 体检入口已改 HTMX 写基座：旧 data-write="custom"/data-write-url 等执行器属性已退役。
		`data-write`, `hx-post="/api/publication/seo-audit"`,
		`hx-target="find [data-seo-audit-result]"`, "data-seo-audit-action",
		"来源统计当前不可用",
		"sitemap / feed 实时状态当前不可用", "外部搜索平台数据当前不可用",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("渲染结果缺少 %q，响应：%s", want, body)
		}
	}
	for _, forbidden := range []string{"window.fetch", "escapeHTML", "issue.Message", "data-msg-clean-lead"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("SEO 页面仍含客户端拼表代码 %q", forbidden)
		}
	}
}

func TestSEOPageTemplateRequiresAuditPermissionForAction(t *testing.T) {
	lang := "zh-CN"
	base := gin.H{
		"menu": "seo", "csrf_token": "test-token",
		"lang": lang, "t": templates.TranslateFunc(lang),
		"langs": templates.LanguageOptions(lang), "lang_redirect": "/admin/seo",
		"Projects": []projectcontract.ProjectResp{}, "SelectedProject": "",
		"Paths": []analyticscontract.PathCount{}, "HasPaths": false, "Buttons": map[string]bool{},
	}
	body := renderAdminTemplate(t, "admin/analytics/seo.html", base)
	if strings.Contains(body, `data-write-url="/api/publication/seo-audit"`) {
		t.Fatal("缺少 seo:audit 权限时不应渲染体检动作")
	}
	if !strings.Contains(body, "需要 SEO 体检权限") {
		t.Fatal("缺少 seo:audit 权限时应解释动作不可用")
	}
}

func readPageRouterSources(dir string) (string, error) {
	chunk, err := os.ReadFile(filepath.Join(dir, "analytics_page_router.go"))
	if err != nil {
		return "", err
	}
	return string(chunk), nil
}

func TestSEORouteIsRegisteredOnAuthenticatedAdminGroup(t *testing.T) {

	body, err := readPageRouterSources(".")
	if err != nil {
		t.Fatalf("读取 analytics 页面路由失败：%v", err)
	}
	if !strings.Contains(body, `pages.GET("/seo",`) {
		t.Fatal("SEO 页面必须注册在已认证的 admin 页面组")
	}

	if !strings.Contains(body, `mw.PageAuthzAs("/api/seo/audit", http.MethodPost)`) {
		t.Fatal("SEO 页面 GET 必须按菜单绑定的 seo:audit 鉴权")
	}
	if !strings.Contains(body, `NewSEOPageHandle(analytics, projects)`) {
		t.Fatal("SEO 页面必须复用 analytics 与 project 契约")
	}
}

func TestSEOPageHandlerUsesAnalyticsContract(t *testing.T) {
	h := NewSEOPageHandle(
		fakeSEOAnalytics{res: &analyticscontract.SummaryResp{
			Paths: []analyticscontract.PathCount{{Path: "/popular", Views: 9, Visitors: 4}},
		}},
		fakeProjects{items: []projectcontract.ProjectResp{{ID: "p-1", Name: "演示站"}}},
	)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/seo", pageview.Slot(SEOPageTemplate, pageview.RenderPage), h.SEOPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/seo", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面请求失败，状态 %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "/popular") {
		t.Fatalf("页面未展示 analytics 契约返回的热门路径：%s", body)
	}
}

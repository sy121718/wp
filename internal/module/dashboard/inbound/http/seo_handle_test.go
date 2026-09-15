package dashboardhttp

// seo_handle_test.go — SEO 控制台的模板与权限边界测试（SEO-020）。

import (
	"context"
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
		"title":         seoPageTitle,
		"menu":          "seo",
		"csrf_token":    "test-token",
		"lang":          lang,
		"t":             templates.TranslateFunc(lang),
		"langs":         templates.LanguageOptions(lang),
		"lang_redirect": "/admin/seo",
		"Projects": []projectcontract.ProjectResp{
			{ID: "p-1", Name: "演示站"},
		},
		"SelectedProject": "p-1",
		"Paths": []analyticscontract.PathCount{
			{Path: "/guides/seo", Views: 42, Visitors: 17},
		},
		"HasPaths": true, "AnalyticsError": false,
		"PermSet": map[string]bool{"seo:audit": true},
	}
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/seo", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/seo.html", data)
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/seo", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"SEO 控制台", "演示站", "/guides/seo", "42", "17",
		"/api/publication/seo-audit", "data-seo-audit-result", "data.issues", "issue.Message",
		"来源统计当前不可用",
		"sitemap / feed 实时状态当前不可用", "外部搜索平台数据当前不可用",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("渲染结果缺少 %q，响应：%s", want, body)
		}
	}
}

func TestSEOPageTemplateRequiresAuditPermissionForAction(t *testing.T) {
	lang := "zh-CN"
	base := gin.H{
		"title": seoPageTitle, "menu": "seo", "csrf_token": "test-token",
		"lang": lang, "t": templates.TranslateFunc(lang),
		"langs": templates.LanguageOptions(lang), "lang_redirect": "/admin/seo",
		"Projects": []projectcontract.ProjectResp{}, "SelectedProject": "",
		"Paths": []analyticscontract.PathCount{}, "HasPaths": false, "AnalyticsError": false, "PermSet": map[string]bool{},
	}
	body := renderAdminTemplate(t, "admin/seo.html", base)
	if strings.Contains(body, "<form method=\"post\" action=\"/api/publication/seo-audit\" data-seo-audit-action>") {
		t.Fatal("缺少 seo:audit 权限时不应渲染体检动作")
	}
	if !strings.Contains(body, "需要 SEO 体检权限") {
		t.Fatal("缺少 seo:audit 权限时应解释动作不可用")
	}
}

func TestSEORouteIsRegisteredOnAuthenticatedAdminGroup(t *testing.T) {
	source, err := os.ReadFile("dashboard_router.go")
	if err != nil {
		t.Fatalf("读取 dashboard 路由失败：%v", err)
	}
	body := string(source)
	if !strings.Contains(body, `adminPages.GET("/seo", seoPages.SEOPage)`) {
		t.Fatal("SEO 页面必须注册在已认证的 adminPages 路由组")
	}
	if !strings.Contains(body, `NewSEOPageHandle(analytics, projects)`) {
		t.Fatal("SEO 页面必须复用 analytics 与 project 契约")
	}
}

func TestSEONavigationRequiresAuditPermission(t *testing.T) {
	withoutPermission := buildNav(map[string]bool{}, "/admin/seo")
	if navHasPath(withoutPermission, "/admin/seo") {
		t.Fatal("缺少 seo:audit 权限时导航不应显示 SEO 控制台")
	}
	withPermission := buildNav(map[string]bool{"seo:audit": true}, "/admin/seo")
	if !navHasPath(withPermission, "/admin/seo") {
		t.Fatal("拥有 seo:audit 权限时导航应显示 SEO 控制台")
	}
}

func navHasPath(groups []navGroup, path string) bool {
	for _, group := range groups {
		for _, node := range group.Nodes {
			if node.Path == path {
				return true
			}
		}
	}
	return false
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
	engine.GET("/admin/seo", h.SEOPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/seo", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面请求失败，状态 %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "/popular") {
		t.Fatalf("页面未展示 analytics 契约返回的热门路径：%s", body)
	}
}

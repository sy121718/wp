package dashboardhttp

// analytics_page_render_test.go — 访问统计页模板的渲染冒烟（BIZ-8）。
//
// 钉住两件事：
//   - admin/analytics.html 的 Jet 语法与 layout 数据契约成立（模板错了只会在用户点开时 500）；
//   - 页面把服务端给的聚合结果真的渲染出来（不是「接口对了、页面空白」）。

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	analyticscontract "go_wp/internal/module/analytics/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

// TestAnalyticsPageTemplateRenders 用真实 Jet 渲染器渲染统计页并断言关键内容。
func TestAnalyticsPageTemplateRenders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lang := "zh-CN"
	data := gin.H{
		"title": analyticsPageTitle, "menu": "analytics",
		"csrf_token": "test-token", "lang": lang,
		"t": templates.TranslateFunc(lang), "langs": templates.LanguageOptions(lang),
		"lang_redirect":   "/admin/analytics",
		"Projects":        []projectcontract.ProjectResp{{ID: "p-1", Name: "演示站"}},
		"SelectedProject": "p-1",
		"FilterFrom":      "2026-01-01", "FilterTo": "2026-01-15",
		"RangeFrom": "2026-01-01", "RangeTo": "2026-01-15",
		"Total": 3, "Visitors": 2, "PathTotal": 2,
		"Daily": []analyticscontract.DailyCount{{Day: "2026-01-15", Views: 3, Visitors: 2}},
		"Paths": []analyticscontract.PathCount{{Path: "/about", Views: 2, Visitors: 1}},
		// 来源域 / 设备分类 / 语言：其中一行故意给空取值，钉住「空值渲染成占位文案」
		// 而不是留一个空格子（空串是合法取值，见 model.CountByDimension）。
		"Referrers": []analyticscontract.RankCount{
			{Value: "ref.example.com", Views: 2, Visitors: 1},
			{Value: "", Views: 1, Visitors: 1},
		},
		"UAClasses":      []analyticscontract.RankCount{{Value: "desktop", Views: 2, Visitors: 2}},
		"Langs":          []analyticscontract.RankCount{{Value: "zh-CN", Views: 3, Visitors: 2}},
		"RankLimit":      20,
		"PaginationInfo": "共 2 条", "PaginationLinks": nil,
		"Err": "",
	}
	engine := gin.New()
	// 模板根目录相对包目录：internal/module/dashboard/inbound/http → internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/analytics", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/analytics.html", data)
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/analytics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"访问统计", "演示站", "2026-01-15", "/about",
		"总浏览数", "独立访客", "按天", "按路径",
		// 三个维度区块（SEO-021 的读路径）—— 页面上真的渲染出来，而不是「接口对了、区块空白」。
		"来源域", "设备分类", "语言",
		"ref.example.com", "desktop", "zh-CN",
		// 空取值渲染为占位文案，不是空格子。
		"（无来源）",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("渲染结果缺少 %q", want)
		}
	}
}

package analyticshttp

// analytics_breakdown_hint_test.go — 「维度分布不可用」提示的判据与渲染。
//
// 场景：保留期清理只删访问明细（page_views），按天汇总（page_views_daily）保留 ——
// 于是已被清理的历史窗口会出现「总数与路径榜有数、三个维度榜全空」。
// 运营看到那种页面只会以为统计坏了，所以判据成立时页面必须给一句解释；
// 而真的没有流量时（总数也是 0）不该提示，否则提示会变成噪音。

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

// TestBreakdownUnavailableJudgement 判据：只有「总数有数 + 三个维度榜全空」才提示。
func TestBreakdownUnavailableJudgement(t *testing.T) {
	rank := func(v string) []analyticscontract.RankCount {
		return []analyticscontract.RankCount{{Value: v, Views: 1, Visitors: 1}}
	}
	cases := []struct {
		name string
		res  *analyticscontract.SummaryResp
		want bool
	}{
		{"nil 响应", nil, false},
		{
			"没有流量（总数为 0、榜也空）",
			&analyticscontract.SummaryResp{Total: 0, BreakdownSource: analyticsdto.SourceDetail},
			false,
		},
		{
			"明细被保留期清理（总数有数、三榜全空）",
			&analyticscontract.SummaryResp{Total: 5, BreakdownSource: analyticsdto.SourceDetail},
			true,
		},
		{
			"来源榜有数据",
			&analyticscontract.SummaryResp{Total: 5, BreakdownSource: analyticsdto.SourceDetail,
				Referrers: rank("ref.example.com")},
			false,
		},
		{
			"只有设备榜有数据（同样不算不可用）",
			&analyticscontract.SummaryResp{Total: 5, BreakdownSource: analyticsdto.SourceDetail,
				UAClasses: rank("desktop")},
			false,
		},
		{
			"只有语言榜有数据",
			&analyticscontract.SummaryResp{Total: 5, BreakdownSource: analyticsdto.SourceDetail,
				Langs: rank("zh-CN")},
			false,
		},
		{
			"维度将来若改走预聚合，则不再判定为不可用",
			&analyticscontract.SummaryResp{Total: 5, BreakdownSource: analyticsdto.SourceSummary},
			false,
		},
	}
	for _, c := range cases {
		if got := breakdownUnavailable(c.res); got != c.want {
			t.Errorf("%s：breakdownUnavailable = %v，期望 %v", c.name, got, c.want)
		}
	}
}

// TestAnalyticsPageRendersBreakdownHint 提示只在判据成立时渲染；键缺失时页面不整页中断。
func TestAnalyticsPageRendersBreakdownHint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lang := "zh-CN"

	renderPage := func(t *testing.T, mutate func(gin.H)) string {
		t.Helper()
		data := gin.H{
			"title": analyticsPageTitle, "menu": "analytics",
			"csrf_token": "test-token", "lang": lang,
			"t": templates.TranslateFunc(lang), "langs": templates.LanguageOptions(lang),
			"lang_redirect":   "/admin/analytics",
			"Projects":        []projectcontract.ProjectResp{{ID: "p-1", Name: "演示站"}},
			"SelectedProject": "p-1",
			"FilterFrom":      "", "FilterTo": "",
			"RangeFrom": "2026-01-01", "RangeTo": "2026-01-15",
			"Total": 0, "Visitors": 0, "PathTotal": 0,
			"Daily":          []analyticscontract.DailyCount{},
			"Paths":          []analyticscontract.PathCount{},
			"Referrers":      []analyticscontract.RankCount{},
			"UAClasses":      []analyticscontract.RankCount{},
			"Langs":          []analyticscontract.RankCount{},
			"RankLimit":      20,
			"PaginationInfo": "共 0 条", "PaginationLinks": nil,
			"Err": "",
		}
		if mutate != nil {
			mutate(data)
		}
		engine := gin.New()
		engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
		engine.GET("/admin/analytics", func(c *gin.Context) {
			c.HTML(http.StatusOK, "admin/analytics/analytics.html", data)
		})
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/analytics", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("页面渲染失败，状态 %d", rec.Code)
		}
		return rec.Body.String()
	}

	const hintText = "该时间段的访问明细已过保留期"
	withHint := renderPage(t, func(d gin.H) { d["BreakdownUnavailable"] = true })
	if !strings.Contains(withHint, hintText) {
		t.Error("判据成立时页面应给出保留期提示，实际未渲染")
	}
	withoutHint := renderPage(t, func(d gin.H) { d["BreakdownUnavailable"] = false })
	if strings.Contains(withoutHint, hintText) {
		t.Error("判据不成立时不该出现保留期提示")
	}
	// 键缺失（别的渲染路径漏传）时，模板必须靠 isset 兜住而不是整页中断 ——
	// 那种中断的表现是 200 + 后半页静默消失，几乎无法从表象定位。
	missing := renderPage(t, func(d gin.H) { delete(d, "BreakdownUnavailable") })
	if !strings.Contains(missing, "来源域") || !strings.Contains(missing, "设备分类") || !strings.Contains(missing, "语言") {
		t.Error("缺少 BreakdownUnavailable 键时，页面后半部分被整块截断了")
	}
}

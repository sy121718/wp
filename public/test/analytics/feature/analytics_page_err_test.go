package feature

import (
	pageview "go_wp/pkg/view"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsenums "go_wp/internal/module/analytics/enums"
	analyticshttp "go_wp/internal/module/analytics/inbound/http"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

const analyticsProjectListErrText = `pq: relation "projects" does not exist (SQLSTATE 42P01)`

type analyticsFailingProjects struct {
	projectcontract.ProjectService
}

func (analyticsFailingProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return nil, errors.New(analyticsProjectListErrText)
}

func serveAnalyticsPageOnce(t *testing.T, handler gin.HandlerFunc, path string) *httptest.ResponseRecorder {
	t.Helper()
	newAnalyticsAuthzEnv(t)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
	engine.GET("/admin/analytics", pageview.Slot(analyticshttp.AnalyticsPageTemplate, adminui.RenderPage), handler)
	engine.GET("/admin/seo", pageview.Slot(analyticshttp.SEOPageTemplate, adminui.RenderPage), handler)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func assertAnalyticsErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, want int, forbid []string) string {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("状态码应 %d，实际 %d：%s", want, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("失败必须是 JSON，实际 Content-Type=%q", ct)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "</html>") {
		t.Errorf("失败时不该渲染任何页面：%s", body)
	}
	if !strings.Contains(body, `"code"`) || !strings.Contains(body, `"message"`) {
		t.Errorf("JSON 出口应含 code 与 message：%s", body)
	}
	for _, bad := range forbid {
		if strings.Contains(body, bad) {
			t.Errorf("响应不应出现 %q：%s", bad, body)
		}
	}
	return body
}

// 工程列表读不出来：真实路由 → 500 JSON，原文不上屏。
func TestAnalyticsPageProjectListFailureReturnsJSON500(t *testing.T) {
	h := analyticshttp.NewAnalyticsPageHandle(nil, analyticsFailingProjects{})
	rec := serveAnalyticsPageOnce(t, h.AnalyticsPage, "/admin/analytics")
	assertAnalyticsErrorResponse(t, rec, http.StatusInternalServerError, []string{
		analyticsProjectListErrText, "SQLSTATE", "42P01", "projects\"",
		analyticsenums.ErrAnalyticsInternal, "uiInternalErr",
		"还没有站点工程", "统计读取失败",
	})
}

// summary 读不出来：同样 500 JSON（页面 GET 失败走统一出口，不渲染失败页）。
type analyticsSummaryErrStub struct {
	analyticscontract.AnalyticsService
	err error
}

func (f analyticsSummaryErrStub) Summary(context.Context, *analyticsdto.SummaryReq) (*analyticscontract.SummaryResp, error) {
	return nil, f.err
}

func TestAnalyticsPageSummaryFailureReturnsJSON(t *testing.T) {
	h := analyticshttp.NewAnalyticsPageHandle(analyticsSummaryErrStub{err: errors.New(analyticsProjectListErrText)}, authzProjects{})
	rec := serveAnalyticsPageOnce(t, h.AnalyticsPage, "/admin/analytics?project=p-1")
	assertAnalyticsErrorResponse(t, rec, http.StatusInternalServerError, []string{
		analyticsProjectListErrText, "SQLSTATE", "42P01", analyticsenums.ErrAnalyticsInternal,
	})
}

// SEO 页两处取数失败同样走尽早退出的 JSON：站点列表失败 / 热门路径失败。
func TestSEOPageFailuresReturnJSON(t *testing.T) {
	t.Run("站点列表失败", func(t *testing.T) {
		p := analyticshttp.NewSEOPageHandle(nil, analyticsFailingProjects{})
		rec := serveAnalyticsPageOnce(t, p.SEOPage, "/admin/seo")
		assertAnalyticsErrorResponse(t, rec, http.StatusInternalServerError, []string{
			analyticsProjectListErrText, "SQLSTATE", "42P01", analyticsenums.ErrAnalyticsInternal,
			"站点列表暂时读取失败",
		})
	})

	t.Run("热门路径失败", func(t *testing.T) {
		p := analyticshttp.NewSEOPageHandle(analyticsSummaryErrStub{err: errors.New(analyticsProjectListErrText)}, authzProjects{})
		rec := serveAnalyticsPageOnce(t, p.SEOPage, "/admin/seo?project=p-1")
		assertAnalyticsErrorResponse(t, rec, http.StatusInternalServerError, []string{
			"SQLSTATE", analyticsenums.ErrAnalyticsInternal, "热门路径暂时读取失败",
		})
	})
}

// 明细过保留期是数据完整性事实，不是读取失败：页面照常 200 渲染。
func TestAnalyticsPageBreakdownUnavailableStillRenders200(t *testing.T) {
	h := analyticshttp.NewAnalyticsPageHandle(retainedAnalytics{}, authzProjects{})
	rec := serveAnalyticsPageOnce(t, h.AnalyticsPage, "/admin/analytics?project=p-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("保留期清理只是完整性事实，页面应 200 渲染，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "</html>") {
		t.Fatal("页面必须渲染完整（缺 </html>）")
	}
}

type retainedAnalytics struct {
	analyticscontract.AnalyticsService
}

func (retainedAnalytics) Summary(context.Context, *analyticsdto.SummaryReq) (*analyticscontract.SummaryResp, error) {
	return &analyticscontract.SummaryResp{
		Total: 45, Visitors: 9, BreakdownSource: analyticsdto.SourceDetail,
	}, nil
}

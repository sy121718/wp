package feature

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsenums "go_wp/internal/module/analytics/enums"
	analyticshttp "go_wp/internal/module/analytics/inbound/http"
	"go_wp/pkg/i18n"
)

var analyticsFacingInjectedEntries = map[string]map[string]string{
	analyticsenums.ErrInvalidRange:      {"zh-CN": "时间范围不合法", "en-US": "EN-INVALID-RANGE"},
	analyticsenums.ErrAnalyticsInternal: {"zh-CN": "统计读取失败", "en-US": "EN-INTERNAL"},
}

// 这一组用例全部打在**真实路由 handler**上：/api/analytics/summary 与两个页面 GET。
// 出口只有一个 —— response.ErrorStatusAuto，controller 不再有取词/分类的包装函数可测。
type analyticsSummaryStub struct {
	err error
}

func (s analyticsSummaryStub) Collect(context.Context, *analyticsdto.CollectReq) error { return nil }

func (s analyticsSummaryStub) Summary(context.Context, *analyticsdto.SummaryReq) (*analyticsdto.SummaryResp, error) {
	return nil, s.err
}

func serveAnalyticsSummary(t *testing.T, err error, lang string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/analytics/summary", analyticshttp.NewHandle(analyticsSummaryStub{err: err}).Summary)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/summary?projectId=p-1", nil)
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	engine.ServeHTTP(rec, req)
	return rec
}

// 业务错误：状态码来自 enums 的声明（400），文案来自词库，裸 key 不上屏。
func TestAnalyticsSummaryBusinessErrorKeepsDeclaredStatusPerLanguage(t *testing.T) {
	i18n.InjectForTest(analyticsFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	for _, tc := range []struct{ lang, want string }{
		{"zh-CN", "时间范围不合法"},
		{"en-US", "EN-INVALID-RANGE"},
	} {
		rec := serveAnalyticsSummary(t, analyticsenums.Err(analyticsenums.ErrInvalidRange), tc.lang)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s：声明 400 的错误应给 400，实际 %d：%s", tc.lang, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s：出口应按词库取词 %q，实际 %s", tc.lang, tc.want, body)
		}
		if strings.Contains(body, analyticsenums.ErrInvalidRange) {
			t.Errorf("%s：不该把裸 key 当 message：%s", tc.lang, body)
		}
	}
}

// 声明的状态码**原样保留**：500 档不被出口改判成 400，也不被改写成别的 key。
func TestAnalyticsSummaryKeepsDeclaredInternalStatus(t *testing.T) {
	i18n.InjectForTest(analyticsFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	rec := serveAnalyticsSummary(t, analyticsenums.Err(analyticsenums.ErrAnalyticsInternal), "en-US")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("声明 500 的错误应给 500（不是 400），实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "EN-INTERNAL") {
		t.Errorf("声明 key 的译文要上屏：%s", rec.Body.String())
	}
}

// 非业务错误（DB 原文）：500 + 归口文案，原文只进日志。
func TestAnalyticsSummaryHidesInternalErrorText(t *testing.T) {
	rec := serveAnalyticsSummary(t, errors.New(`pq: relation "page_views" does not exist`), "en-US")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("非业务错误应给 500，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, bad := range []string{"page_views", "pq:", analyticsenums.ErrAnalyticsInternal} {
		if strings.Contains(body, bad) {
			t.Errorf("内部原文/裸 key 不该上屏（%q）：%s", bad, body)
		}
	}
}

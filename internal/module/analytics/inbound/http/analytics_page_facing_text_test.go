package analyticshttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	analyticsenums "go_wp/internal/module/analytics/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

var analyticsFacingInjectedEntries = map[string]map[string]string{
	analyticsenums.ErrInvalidRange:      {"zh-CN": "时间范围不合法", "en-US": "EN-INVALID-RANGE"},
	analyticsenums.ErrAnalyticsInternal: {"zh-CN": "统计服务内部错误", "en-US": "EN-INTERNAL"},
}

func analyticsFacingLangCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/analytics", nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c
}

func TestAnalyticsFacingErrorTranslatesWhitelistHit(t *testing.T) {
	i18n.InjectForTest(analyticsFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	key := analyticsenums.ErrInvalidRange

	if got := analyticsFacingError(analyticsFacingLangCtx(t, "zh-CN"), errors.New(key)); got != "时间范围不合法" {
		t.Errorf("zh-CN 命中应给译文，实际 %q（等于 key 说明少了取词那一层）", got)
	}
	if got := analyticsFacingError(analyticsFacingLangCtx(t, "en-US"), errors.New(key)); got != "EN-INVALID-RANGE" {
		t.Errorf("en-US 命中应给英文译文，实际 %q", got)
	}
}

func TestAnalyticsFacingErrorFallsBackSafely(t *testing.T) {
	i18n.InjectForTest(analyticsFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	c := analyticsFacingLangCtx(t, "en-US")

	if got := analyticsFacingError(c, errors.New(`pq: relation "page_views" does not exist`)); got != "EN-INTERNAL" {
		t.Errorf("未命中应落归口译文，实际 %q", got)
	}
	if got := analyticsFacingError(c, nil); got != "" {
		t.Errorf("nil error 应给空串，实际 %q", got)
	}
}

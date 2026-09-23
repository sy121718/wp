package analyticshttp

// analytics_page_facing_text_test.go — 统计页「命中白名单后显示的是译文、不是裸 key」的回归。
//
// 根因（2026-09，与 user / order / page 各域同源）：analyticsenums 的值是 i18n **item_key**
// （ErrInvalidParam / ErrInvalidRange —— PascalCase 形态，词条在 058 与 429），而
// analyticsFacingError 命中白名单那一支原样 `return allowed`，返回值随后写进模板数据的
// "Err"（analytics_page_handle.go）—— {{.Err}} 是**直接渲染**的文本、不经过 pkg/response
// 的 translate，于是运营看到的是 `ErrInvalidRange` 这样的裸 key。
//
// 修法：判定保持唯一（analyticsFacingMessages），命中那一支多一层取词
// （shell.TranslateFor），fallback 给归口兜底文案 —— 词条缺失时显示一句人话而不是裸 key。
// 未命中的归口那一支不动（它早已经 analyticsInternalText 取词）。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条（英文值带 EN- 前缀，与中文毫无相似度）：
// 不建库、不起装配，任何一处没按当前语言取词都会立刻暴露。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	analyticsenums "go_wp/internal/module/analytics/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// analyticsFacingInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var analyticsFacingInjectedEntries = map[string]map[string]string{
	analyticsenums.ErrInvalidRange:      {"zh-CN": "时间范围不合法", "en-US": "EN-INVALID-RANGE"},
	analyticsenums.ErrAnalyticsInternal: {"zh-CN": "统计服务内部错误", "en-US": "EN-INTERNAL"},
}

// analyticsFacingLangCtx 构造一个绑定了 Accept-Language 的上下文。
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

// TestAnalyticsFacingErrorTranslatesWhitelistHit 命中白名单的那一支按当前语言取词。
func TestAnalyticsFacingErrorTranslatesWhitelistHit(t *testing.T) {
	i18n.InjectForTest(analyticsFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	key := analyticsenums.ErrInvalidRange

	// 命中白名单：给译文，**不能**把 key 本身摆到页面上。
	if got := analyticsFacingError(analyticsFacingLangCtx(t, "zh-CN"), errors.New(key)); got != "时间范围不合法" {
		t.Errorf("zh-CN 命中应给译文，实际 %q（等于 key 说明少了取词那一层）", got)
	}
	if got := analyticsFacingError(analyticsFacingLangCtx(t, "en-US"), errors.New(key)); got != "EN-INVALID-RANGE" {
		t.Errorf("en-US 命中应给英文译文，实际 %q", got)
	}
}

// TestAnalyticsFacingErrorFallsBackSafely 未命中落归口译文；nil 落空串。
func TestAnalyticsFacingErrorFallsBackSafely(t *testing.T) {
	i18n.InjectForTest(analyticsFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	c := analyticsFacingLangCtx(t, "en-US")

	// 未命中白名单：落归口译文（原文只进日志，不进页面）。
	if got := analyticsFacingError(c, errors.New(`pq: relation "page_views" does not exist`)); got != "EN-INTERNAL" {
		t.Errorf("未命中应落归口译文，实际 %q", got)
	}
	if got := analyticsFacingError(c, nil); got != "" {
		t.Errorf("nil error 应给空串，实际 %q", got)
	}
}

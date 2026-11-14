package membershiphttp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	membershipenums "go_wp/internal/module/membership/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

var membershipFacingInjectedEntries = map[string]map[string]string{
	membershipenums.ErrTierNameTaken:      {"zh-CN": "等级名已被占用", "en-US": "EN-NAME-TAKEN"},
	membershipenums.ErrDefaultTierMissing: {"zh-CN": "该工程没有默认等级", "en-US": "EN-NO-DEFAULT-TIER"},
	"membership.err.internal":             {"zh-CN": "服务内部错误", "en-US": "EN-INTERNAL"},
}

func membershipFacingCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/membership/tiers?lang="+lang, nil)
	return c
}

func membershipJSONRecorder(t *testing.T, lang string, err error) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/admin/api/membership/tier/create", pageview.HTMLFragment, func(c *gin.Context) {
		membershipFail(c, err)
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/admin/api/membership/tier/create?lang="+lang, nil))

	// 失败出口已改为 HTML 片段：状态码与文案都从响应体里读。
	body := rec.Body.String()
	text := body
	if start := strings.Index(body, "<p>"); start >= 0 {
		if end := strings.Index(body[start:], "</p>"); end > 0 {
			text = body[start+3 : start+end]
		}
	}
	return rec.Code, strings.TrimSpace(text)
}

func TestMembershipJSONResolvesKeyByRequestLanguage(t *testing.T) {
	i18n.InjectForTest(membershipFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	err := membershipenums.ErrDetail(membershipenums.ErrDefaultTierMissing, "p-1")

	code, msg := membershipJSONRecorder(t, "en-US", err)
	if code != http.StatusBadRequest {
		t.Errorf("状态码 = %d，期望 400（业务参数类）", code)
	}
	// 失败区只播报一句：定位信息（这里是工程 id）属业务数据，随响应 data 返回，
	// 不拼进提示句 —— 判据见 admin/errors/failure.html 的注释。
	if msg != "EN-NO-DEFAULT-TIER" {
		t.Errorf("[en-US] message = %q，期望取英文词条且不拼定位信息", msg)
	}

	_, zhMsg := membershipJSONRecorder(t, "zh-CN", err)
	if zhMsg != "该工程没有默认等级" {
		t.Errorf("[zh-CN] message = %q，期望取中文词条且不拼定位信息", zhMsg)
	}
	if strings.Contains(zhMsg, membershipenums.ErrDefaultTierMissing) {
		t.Errorf("[zh-CN] message 里出现裸 key：%q", zhMsg)
	}
}

func TestMembershipJSONInternalErrorUsesFacingKey(t *testing.T) {
	i18n.InjectForTest(membershipFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	code, msg := membershipJSONRecorder(t, "en-US",
		errString(`pq: relation "membership_tiers" does not exist`))
	if code != http.StatusInternalServerError {
		t.Errorf("状态码 = %d，期望 500", code)
	}
	if msg == "" || strings.Contains(msg, "membership_tiers") || strings.Contains(msg, "pq:") {
		t.Errorf("message = %q，期望归口词条（不泄漏原文）", msg)
	}
}

func TestMembershipTextGivesLocalizedSentence(t *testing.T) {
	i18n.InjectForTest(membershipFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	err := membershipenums.ErrDetail(membershipenums.ErrDefaultTierMissing, "p-1")
	if got, want := membershipText(membershipFacingCtx(t, "zh-CN"), err), "该工程没有默认等级：p-1"; got != want {
		t.Fatalf("中文页面出口 = %q，期望 %q", got, want)
	}
	if got, want := membershipText(membershipFacingCtx(t, "en-US"), err), "EN-NO-DEFAULT-TIER: p-1"; got != want {
		t.Fatalf("英文页面出口 = %q，期望 %q", got, want)
	}
}

func TestMembershipTextInternalErrorStaysGeneric(t *testing.T) {
	i18n.InjectForTest(membershipFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	got := membershipText(membershipFacingCtx(t, "en-US"),
		errString(`pq: duplicate key value violates unique constraint "uq_membership_tiers_name"`))
	if strings.Contains(got, "uq_membership_tiers_name") || strings.Contains(got, "pq:") {
		t.Fatalf("内部原文泄漏到页面文案：%q", got)
	}

	if got != response.InternalFallback {
		t.Errorf("页面出口 = %q，期望共享归口文案", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestMembershipDetailFallbackDoesNotDuplicate(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	err := membershipenums.ErrDetail(membershipenums.ErrDefaultTierMissing, "project-unique-marker")
	code, got := membershipJSONRecorder(t, "zh-CN", err)
	// 词库清空后，businessText 在 out == fact.Key 且 fact.Fallback == fact.Key 时
	// 走 logMissingMessage 并交出共享归口文案（pkg/response.InternalFallback）。
	// 缺词时失败片段退成该状态码档的通用文案，内部定位信息不上屏。
	want := "参数有误，请检查后重试"
	if code != http.StatusBadRequest || got != want {
		t.Fatalf("缺词时应退成通用文案: code=%d got=%q want=%q", code, got, want)
	}
	if n := strings.Count(got, "project-unique-marker"); n != 0 {
		t.Fatalf("缺词时内部定位信息不得上屏，实际出现 %d 次：%q", n, got)
	}
	if strings.Contains(got, membershipenums.ErrDefaultTierMissing) {
		t.Fatalf("message 里出现裸 key：%q", got)
	}
}

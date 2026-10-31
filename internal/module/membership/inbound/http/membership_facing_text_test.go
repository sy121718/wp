package membershiphttp

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	membershipenums "go_wp/internal/module/membership/enums"
	"go_wp/pkg/i18n"
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
	engine.POST("/admin/api/membership/tier/create", func(c *gin.Context) {
		membershipFail(c, err)
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/admin/api/membership/tier/create?lang="+lang, nil))

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON：%s", rec.Body.String())
	}
	return body.Code, body.Message
}

func TestMembershipJSONResolvesKeyByRequestLanguage(t *testing.T) {
	i18n.InjectForTest(membershipFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	err := membershipenums.ErrDetail(membershipenums.ErrDefaultTierMissing, "p-1")

	code, msg := membershipJSONRecorder(t, "en-US", err)
	if code != http.StatusBadRequest {
		t.Errorf("状态码 = %d，期望 400（业务参数类）", code)
	}
	if msg != "EN-NO-DEFAULT-TIER: p-1" {
		t.Errorf("[en-US] message = %q，期望前缀取英文词条、定位信息保留", msg)
	}

	_, zhMsg := membershipJSONRecorder(t, "zh-CN", err)
	if zhMsg != "该工程没有默认等级：p-1" {
		t.Errorf("[zh-CN] message = %q（中文用全角分隔符）", zhMsg)
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

	if got != "服务器内部错误，请稍后重试" {
		t.Errorf("页面出口 = %q，期望归口文案", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestMembershipDetailFallbackDoesNotDuplicate(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	err := membershipenums.ErrDetail(membershipenums.ErrDefaultTierMissing, "project-unique-marker")
	code, got := membershipJSONRecorder(t, "zh-CN", err)
	want := membershipenums.FallbackFor(membershipenums.ErrDefaultTierMissing) + "：project-unique-marker"
	if code != http.StatusBadRequest || got != want {
		t.Fatalf("兜底与唯一定位信息应保持: code=%d got=%q want=%q", code, got, want)
	}
}

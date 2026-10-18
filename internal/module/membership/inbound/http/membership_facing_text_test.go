package membershiphttp

// membership_facing_text_test.go — 「业务错误按请求语言出文案、定位信息不丢」的回归（BIZ-3）。
//
// 判据只有一份（pkg/facing.FactOf），两个出口都用它：
//
//	JSON 出口 membershipFail → 状态码按业务 key 分档，文案取本次请求语言的词条；
//	页面出口 membershipText → 同一句话，拼进整页失败槽 / 片段提示条。
//
// 带定位信息（Tail）时两边都要把明细拼回去：那是给操作者的数据（哪个 project_id、
// 哪个等级名），吞掉它等于让人拿不到可行动的信息。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条（中英值互不相似、英文带 EN- 前缀）：
// 不建库、不起装配，任何一处「该取词的地方给了裸 key」都会立刻暴露。

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

// membershipFacingInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var membershipFacingInjectedEntries = map[string]map[string]string{
	membershipenums.ErrTierNameTaken:      {"zh-CN": "等级名已被占用", "en-US": "EN-NAME-TAKEN"},
	membershipenums.ErrDefaultTierMissing: {"zh-CN": "该工程没有默认等级", "en-US": "EN-NO-DEFAULT-TIER"},
	"membership.err.internal":             {"zh-CN": "服务内部错误", "en-US": "EN-INTERNAL"},
}

// membershipFacingCtx 构造一个绑定了语言的上下文（走 query 语言协商，最直观）。
func membershipFacingCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/membership/tiers?lang="+lang, nil)
	return c
}

// membershipJSONRecorder 跑一次「与 handler 同形」的 JSON 响应路径。
//
// 刻意照抄 handle 的写法（membershipFail(c, err)）：
// 本批改的正是这一段链路上的取词与状态码，用别的方式拼响应就不算覆盖。
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

// TestMembershipJSONResolvesKeyByRequestLanguage 同一条业务 key 按请求语言解析。
//
// 断言的是**最终响应体**：前缀被翻成该请求语言的词条，定位信息原样保留 ——
// 这正是「JSON 响应提示依照系统语言解析」的可观测形态。
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

// TestMembershipJSONInternalErrorUsesFacingKey 内部错误在 JSON 链上只剩归口文案。
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

// TestMembershipTextGivesLocalizedSentence 页面出口给的是成品语句（服务端渲染，模板只放进提示条）。
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

// TestMembershipTextInternalErrorStaysGeneric 页面出口对内部错误同样只用归口文案。
func TestMembershipTextInternalErrorStaysGeneric(t *testing.T) {
	i18n.InjectForTest(membershipFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	got := membershipText(membershipFacingCtx(t, "en-US"),
		errString(`pq: duplicate key value violates unique constraint "uq_membership_tiers_name"`))
	if strings.Contains(got, "uq_membership_tiers_name") || strings.Contains(got, "pq:") {
		t.Fatalf("内部原文泄漏到页面文案：%q", got)
	}
	// 归口文案是 pkg/response 的一句话（不是枚举 key）：页面直接渲染它，无需再取词。
	if got != "服务器内部错误，请稍后重试" {
		t.Errorf("页面出口 = %q，期望归口文案", got)
	}
}

// errString 最小 error 实现（测试里造带定位信息的业务错误）。
type errString string

func (e errString) Error() string { return string(e) }

package membershiphttp

// membership_facing_text_test.go — 「业务响应给枚举 key、由 response 按系统语言解析」的回归（BIZ-3）。
//
// 两条出口的责任不同，本文件把两边都钉住：
//
//	JSON 出口 membershipErrText → 只回**枚举 key**（带定位信息时归一成半角「: 」），
//	  取词由 pkg/response 的 translate 按**请求方语言**做 —— 前端 / 移动端要能按自己的
//	  系统语言渲染，响应里就不能躺着一句服务端已经定好的中文；
//	页面出口 membershipErrPageText → 直接回当前语言下成品的一句（服务端渲染，模板只放进提示条）。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条（中英值互不相似、英文带 EN- 前缀）：
// 不建库、不起装配，任何一处「该给 key 的地方给了译文」或「该取词的地方给了裸 key」
// 都会立刻暴露。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	membershipenums "go_wp/internal/module/membership/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
)

// membershipFacingInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var membershipFacingInjectedEntries = map[string]map[string]string{
	membershipenums.ErrTierNameTaken:      {"zh-CN": "等级名已被占用", "en-US": "EN-NAME-TAKEN"},
	membershipenums.ErrDefaultTierMissing: {"zh-CN": "该工程没有默认等级", "en-US": "EN-NO-DEFAULT-TIER"},
	membershipenums.ErrInternal:           {"zh-CN": "服务内部错误", "en-US": "EN-INTERNAL"},
}

// membershipFacingCtx 构造一个绑定了语言的上下文（走 query 语言协商，最直观）。
func membershipFacingCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/membership/tiers?lang="+lang, nil)
	return c
}

// TestMembershipErrTextKeepsEnumKeyOfWhitelistHit JSON 出口回的是枚举 key，不是译文。
//
// 这条是「业务响应文案与 UI 文案分开」的直接判据：在这个出口上取词，
// 等于把一句话定型成服务端当时的语言，前端再换语言也改不回来。
func TestMembershipErrTextKeepsEnumKeyOfWhitelistHit(t *testing.T) {
	i18n.InjectForTest(membershipFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	key := membershipenums.ErrTierNameTaken
	for _, lang := range []string{"zh-CN", "en-US"} {
		got := membershipErrText(membershipFacingCtx(t, lang), errString(key))
		if got != key {
			t.Errorf("[%s] JSON 出口应回枚举 key %q，实际 %q", lang, key, got)
		}
		if got == "等级名已被占用" || got == "EN-NAME-TAKEN" {
			t.Errorf("[%s] JSON 出口取了词（文案被定型成服务端语言）", lang)
		}
	}
}

// TestMembershipErrTextNormalizesDetailSeparator detail 形态归一成半角「: 」。
//
// 本模块写侧固定拼全角「：」（enums.WithDetail），而 response 的 translate 只认半角 ——
// 不归一的话整串都不命中词条，接口里会原样出现 `membership.err.defaultTierMissing：42`。
func TestMembershipErrTextNormalizesDetailSeparator(t *testing.T) {
	i18n.InjectForTest(membershipFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	got := membershipErrText(membershipFacingCtx(t, "en-US"),
		errString(membershipenums.WithDetail(membershipenums.ErrDefaultTierMissing, "42")))
	if want := membershipenums.ErrDefaultTierMissing + ": 42"; got != want {
		t.Fatalf("归一后的形态 = %q，期望 %q", got, want)
	}
	if strings.Contains(got, membershipenums.FacingDetailSep) {
		t.Errorf("全角分隔符会让 translate 的形态 3 失配，前端看到裸 key：%q", got)
	}
}

// TestMembershipErrInternalIsKeyAndNotLeaked 未命中白名单 → 归口 key（内部原文只进日志）。
func TestMembershipErrInternalIsKeyAndNotLeaked(t *testing.T) {
	i18n.InjectForTest(membershipFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	got := membershipErrText(membershipFacingCtx(t, "en-US"),
		errString(`pq: duplicate key value violates unique constraint "uq_membership_tiers_name"`))
	if got != membershipenums.ErrInternal {
		t.Fatalf("内部错误应回归口 key，实际 %q", got)
	}
	if strings.Contains(got, "uq_membership_tiers_name") || strings.Contains(got, "pq:") {
		t.Fatalf("内部原文泄漏到响应文案：%q", got)
	}
}

// membershipJSONRecorder 跑一次「与 handler 同形」的 JSON 响应路径。
//
// 刻意照抄 handle 的写法（response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))）：
// 本批改的正是这一段链路上的取词责任，用别的方式拼响应就不算覆盖。
func membershipJSONRecorder(t *testing.T, lang string, err error) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/admin/api/membership/tier/create", func(c *gin.Context) {
		response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))
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

// TestMembershipJSONResponseResolvesKeyByRequestLanguage 同一条业务 key 按请求语言解析。
//
// 这里断言的是**最终响应体**：前缀被翻成该请求语言的词条，定位信息原样保留 ——
// 这正是「JSON 响应提示依照系统语言解析枚举 key」的可观测形态。
func TestMembershipJSONResponseResolvesKeyByRequestLanguage(t *testing.T) {
	i18n.InjectForTest(membershipFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	err := errString(membershipenums.WithDetail(membershipenums.ErrDefaultTierMissing, "p-1"))

	code, msg := membershipJSONRecorder(t, "en-US", err)
	if code != http.StatusBadRequest {
		t.Errorf("状态码 = %d，期望 400（业务参数类）", code)
	}
	if msg != "EN-NO-DEFAULT-TIER: p-1" {
		t.Errorf("[en-US] message = %q，期望前缀取英文词条、定位信息保留", msg)
	}

	_, zhMsg := membershipJSONRecorder(t, "zh-CN", err)
	if zhMsg != "该工程没有默认等级: p-1" {
		t.Errorf("[zh-CN] message = %q", zhMsg)
	}
	if strings.Contains(zhMsg, membershipenums.ErrDefaultTierMissing) {
		t.Errorf("[zh-CN] message 里出现裸 key：%q", zhMsg)
	}
}

// TestMembershipJSONInternalErrorUsesFacingKey 内部错误在 JSON 链上同样只剩归口文案。
func TestMembershipJSONInternalErrorUsesFacingKey(t *testing.T) {
	i18n.InjectForTest(membershipFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	code, msg := membershipJSONRecorder(t, "en-US",
		errString(`pq: relation "membership_tiers" does not exist`))
	if code != http.StatusInternalServerError {
		t.Errorf("状态码 = %d，期望 500", code)
	}
	if msg != "EN-INTERNAL" {
		t.Errorf("message = %q，期望归口词条（不泄漏原文）", msg)
	}
}

// TestMembershipErrPageTextGivesLocalizedSentence 页面出口给的是成品语句（与 JSON 出口相反）。
//
// 页面由服务端渲染，语言由本次请求决定 —— 模板只负责把这一句放进提示条，
// 所以这里必须已经取过词（否则运营看到的是裸 key）。
func TestMembershipErrPageTextGivesLocalizedSentence(t *testing.T) {
	i18n.InjectForTest(membershipFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	err := errString(membershipenums.WithDetail(membershipenums.ErrDefaultTierMissing, "p-1"))
	got := membershipErrPageText(membershipFacingCtx(t, "zh-CN"), err)
	// 中文页面用全角分隔符（facingSep），与 UI 的排版口径一致。
	if want := "该工程没有默认等级" + membershipenums.FacingDetailSep + "p-1"; got != want {
		t.Fatalf("页面出口 = %q，期望 %q", got, want)
	}
	got = membershipErrPageText(membershipFacingCtx(t, "en-US"), err)
	if want := "EN-NO-DEFAULT-TIER" + membershipErrSepHalf + "p-1"; got != want {
		t.Fatalf("英文页面出口 = %q，期望 %q", got, want)
	}
}

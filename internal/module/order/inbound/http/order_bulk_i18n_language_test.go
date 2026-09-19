package orderhttp

// order_bulk_i18n_language_test.go — 批量结论文案的**当前语言一致性**回归。
//
// 为什么必须有这一条：写侧（bulkSummary）与读侧（orderDoneTexts）共用 orderBulkTextOf
// 这一个取法，但「共用」是结构事实、不是可观测事实 —— 只要有人把读侧候选重新写成中文常量
// （或忘了带 c），中文环境下一切正常，英文页面上真实的回执却被 shell.FacingNotice 判成伪造
// 而**静默消失**（?done= 落空串），既没有报错也没有日志。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条（值带 EN- 前缀，与中文毫无相似度）：
// 不建库、不起装配，任何一处没按当前语言取词都会立刻暴露。测完还原空缓存。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// orderBulkInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var orderBulkInjectedEntries = map[string]map[string]string{
	orderenums.BulkNoneSelected:        {"zh-CN": "没有勾选任何%s。", "en-US": "EN-NONE %s"},
	orderenums.BulkAllDone:             {"zh-CN": "%s %s 个%s。", "en-US": "EN-DONE %s %s %s"},
	orderenums.BulkAllSkipped:          {"zh-CN": "0 个%s%s，%s 个被跳过（状态不允许或已不存在）。", "en-US": "EN-SKIP %s %s %s"},
	orderenums.BulkPartial:             {"zh-CN": "%s %s 个%s，跳过 %s 个（状态不允许或已不存在）。", "en-US": "EN-PART %s %s %s %s"},
	orderenums.BulkVerbFlowed:          {"zh-CN": "已流转", "en-US": "EN-FLOWED"},
	orderenums.BulkVerbCancelled:       {"zh-CN": "已取消", "en-US": "EN-CANCELLED"},
	orderenums.BulkVerbApproved:        {"zh-CN": "已同意", "en-US": "EN-APPROVED"},
	orderenums.BulkVerbRejected:        {"zh-CN": "已拒绝", "en-US": "EN-REJECTED"},
	orderenums.BulkVerbDeleted:         {"zh-CN": "已删除", "en-US": "EN-DELETED"},
	orderenums.BulkVerbDisabled:        {"zh-CN": "已停用", "en-US": "EN-DISABLED"},
	orderenums.BulkVerbEnabled:         {"zh-CN": "已启用", "en-US": "EN-ENABLED"},
	orderenums.BulkNounOrder:           {"zh-CN": "订单", "en-US": "EN-ORDER"},
	orderenums.BulkNounReturn:          {"zh-CN": "退货申请", "en-US": "EN-RETURN"},
	orderenums.BulkNounCoupon:          {"zh-CN": "优惠码", "en-US": "EN-COUPON"},
	orderenums.BulkCouponTargetInvalid: {"zh-CN": "目标状态不合法，本次没有处理任何优惠码。", "en-US": "EN-TARGET-INVALID"},
}

// orderBulkLangCtx 构造一个绑定了 Accept-Language 的上下文。
func orderBulkLangCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/orders", nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c
}

// TestOrderBulkNoticeFollowsRequestLanguage 四个分支 + 动词 + 名词都随当前语言，且读侧认得。
func TestOrderBulkNoticeFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(orderBulkInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	en := orderBulkLangCtx(t, "en-US")
	cases := []struct {
		name    string
		done    int
		skipped int
		want    string
	}{
		{"未勾选", 0, 0, "EN-NONE EN-ORDER"},
		{"全成功", 3, 0, "EN-DONE EN-FLOWED 3 EN-ORDER"},
		{"全部被跳过", 0, 2, "EN-SKIP EN-ORDER EN-FLOWED 2"},
		{"部分成功", 3, 2, "EN-PART EN-FLOWED 3 EN-ORDER 2"},
	}
	for _, tc := range cases {
		msg := bulkSummary(en, orderBulkVerbFlowed, orderBulkNounOrder, tc.done, tc.skipped)
		if msg != tc.want {
			t.Errorf("%s：应按当前语言拼装，got %q want %q", tc.name, msg, tc.want)
			continue
		}
		if got := orderPageDone(en, msg); got != msg {
			t.Errorf("%s：英文页面上这条回执被读侧判成伪造（会静默消失）：got %q want %q", tc.name, got, msg)
		}
	}

	// 参数级回执（批量启停优惠码的目标状态非法）同样走当前语言。
	targetInvalid := orderBulkTextOf(en, couponBulkTargetInvalidText)
	if targetInvalid != "EN-TARGET-INVALID" {
		t.Fatalf("参数级回执应按当前语言取词，实际 %q", targetInvalid)
	}
	if got := orderPageDone(en, targetInvalid); got != targetInvalid {
		t.Errorf("英文参数级回执被读侧判成伪造：got %q want %q", got, targetInvalid)
	}

	// 中文请求走中文词条（每一条都要覆盖，漏一条就说明该处没按语言取词）。
	zh := orderBulkLangCtx(t, "zh-CN")
	if msg := bulkSummary(zh, orderBulkVerbFlowed, orderBulkNounOrder, 3, 0); msg != "已流转 3 个订单。" {
		t.Errorf("中文请求应取中文模板与中文动词/名词，实际 %q", msg)
	}
}

package orderhttp

// coupon_page_facing_text_test.go — 优惠码页「命中白名单后页面显示的是译文、不是裸 key」的回归。
//
// 根因（2026-09 实测，与 order / return 两页同源）：白名单里存的是 i18n **item_key**
// （orderenums.MsgCouponCreated = "order.msg.couponCreated"），而页面出口是模板直接渲染
// {{.Ok}} / {{.Err}}，**不经过 pkg/response 的 translate** —— 只放行 key 的话，
// 运营保存优惠码之后提示条上显示的就是那串 key 本身（「优惠码已更新」这句现成的译文永远到不了眼前）。
//
// 修法照 return_page_query.go 的样板：**判定只有一份**（couponFacingText，API 出口需要 key），
// 页面出口多一层取词（couponPageFacingText）。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条（英文值带 EN- 前缀，与中文毫无相似度）：
// 不建库、不起装配，任何一处没按当前语言取词都会立刻暴露。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// couponFacingInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var couponFacingInjectedEntries = map[string]map[string]string{
	orderenums.MsgCouponCreated:   {"zh-CN": "优惠码已创建", "en-US": "EN-COUPON-CREATED"},
	orderenums.ErrCouponCodeTaken: {"zh-CN": "这个优惠码已经存在", "en-US": "EN-COUPON-TAKEN"},
}

// couponFacingLangCtx 构造一个绑定了 Accept-Language 的上下文。
func couponFacingLangCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/coupons", nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c
}

// TestCouponPageFacingTextTranslatesWhitelistHit 命中白名单的那一支按当前语言取词。
func TestCouponPageFacingTextTranslatesWhitelistHit(t *testing.T) {
	i18n.InjectForTest(couponFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	// 判定出口保持「原样返回 key」：API 出口需要 key，交给 pkg/response 翻译。
	if got := couponFacingText(orderenums.MsgCouponCreated); got != orderenums.MsgCouponCreated {
		t.Fatalf("判定出口应原样返回 key（API 出口交给 pkg/response 翻译），实际 %q", got)
	}

	// 页面出口：同一份判定 + 按当前语言取词。
	if got := couponPageFacingText(couponFacingLangCtx(t, "zh-CN"))(orderenums.MsgCouponCreated); got != "优惠码已创建" {
		t.Errorf("zh-CN 页面出口应给译文，实际 %q（裸 key 说明少了取词那一层）", got)
	}
	if got := couponPageFacingText(couponFacingLangCtx(t, "en-US"))(orderenums.MsgCouponCreated); got != "EN-COUPON-CREATED" {
		t.Errorf("en-US 页面出口应给英文译文，实际 %q", got)
	}

	// 错误出口同样取词（couponFacingError 是重定向 ?err= 的那一支）。
	if got := couponFacingError(couponFacingLangCtx(t, "en-US"), errors.New(orderenums.ErrCouponCodeTaken)); got != "EN-COUPON-TAKEN" {
		t.Errorf("错误出口应给英文译文，实际 %q", got)
	}
}

// TestCouponPageFacingTextKeepsNonWhitelistOut 非白名单原文与自造中文常量都不受影响。
func TestCouponPageFacingTextKeepsNonWhitelistOut(t *testing.T) {
	i18n.InjectForTest(couponFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	// 未命中白名单：落空串，由调用方换归口文案（绝不把数据库原文带上页面）。
	if got := couponPageFacingText(couponFacingLangCtx(t, "en-US"))(`pq: relation "coupons" does not exist`); got != "" {
		t.Errorf("非白名单原文应落空串，实际 %q", got)
	}
	// 本页自造的中文常量：按 key 查不到词条，取词函数据 fallback 原样返回（中英界面都是它）。
	for _, lang := range []string{"zh-CN", "en-US"} {
		if got := couponPageFacingText(couponFacingLangCtx(t, lang))(couponForeignLabel.fallback); got != couponForeignLabel.fallback {
			t.Errorf("%s：自造中文文案应原样返回，实际 %q", lang, got)
		}
	}
}

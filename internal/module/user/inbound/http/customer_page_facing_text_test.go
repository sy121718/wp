package userhttp

// customer_page_facing_text_test.go — 后台客户页「命中白名单后页面显示的是译文、不是裸 key」的回归。
//
// 根因（2026-09，与 order / return 两页同源）：userenums.UserFacingMessages 里存的是 i18n
// **item_key**（user.msg.customerDisabled / user.err.userNotFound …），而 customers.html 与
// customer_detail.html 里的 {{.Err}} / {{.Ok}} 是**直接渲染**的文本、不经过 pkg/response 的
// translate —— 只放行 key 的话，运营停用一个账号后提示条上显示的就是
// 「上一次操作未完成：user.msg.customerDisabled」。
//
// 修法照 return_page_query.go 的样板：**判定只有一份**（customerFacingText），
// 页面出口多一层取词（customerPageFacingText）。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条（英文值带 EN- 前缀，与中文毫无相似度）：
// 不建库、不起装配，任何一处没按当前语言取词都会立刻暴露。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// customerFacingInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var customerFacingInjectedEntries = map[string]map[string]string{
	userenums.MsgCustomerDisabled:      {"zh-CN": "账号已停用", "en-US": "EN-CUSTOMER-DISABLED"},
	userenums.ErrCustomerStatusInvalid: {"zh-CN": "账号状态取值不合法", "en-US": "EN-STATUS-INVALID"},
}

// customerFacingLangCtx 构造一个绑定了 Accept-Language 的上下文。
func customerFacingLangCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/customers", nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c
}

// TestCustomerPageFacingTextTranslatesWhitelistHit 命中白名单的那一支按当前语言取词。
func TestCustomerPageFacingTextTranslatesWhitelistHit(t *testing.T) {
	i18n.InjectForTest(customerFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	// 判定出口保持「原样返回 key」：白名单里存的就是 item_key。
	if got := customerFacingText(userenums.MsgCustomerDisabled); got != userenums.MsgCustomerDisabled {
		t.Fatalf("判定出口应原样返回 key，实际 %q", got)
	}

	// 页面出口（?err= / ?ok= 回显）：同一份判定 + 按当前语言取词。
	if got := customerPageFacingText(customerFacingLangCtx(t, "zh-CN"))(userenums.MsgCustomerDisabled); got != "账号已停用" {
		t.Errorf("zh-CN 页面出口应给译文，实际 %q（裸 key 说明少了取词那一层）", got)
	}
	if got := customerPageFacingText(customerFacingLangCtx(t, "en-US"))(userenums.MsgCustomerDisabled); got != "EN-CUSTOMER-DISABLED" {
		t.Errorf("en-US 页面出口应给英文译文，实际 %q", got)
	}

	// 错误出口同样取词（customerFacingError 是列表页 / 详情页装载失败的那一支）。
	if got := customerFacingError(customerFacingLangCtx(t, "en-US"), errors.New(userenums.ErrCustomerStatusInvalid)); got != "EN-STATUS-INVALID" {
		t.Errorf("错误出口应给英文译文，实际 %q", got)
	}
}

// TestCustomerPageFacingTextKeepsNonWhitelistOut 非白名单原文与自造中文常量都不受影响。
func TestCustomerPageFacingTextKeepsNonWhitelistOut(t *testing.T) {
	i18n.InjectForTest(customerFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	// 未命中白名单：落空串，由调用方换归口文案（绝不把数据库原文带上页面）。
	if got := customerPageFacingText(customerFacingLangCtx(t, "en-US"))(`pq: relation "users" does not exist`); got != "" {
		t.Errorf("非白名单原文应落空串，实际 %q", got)
	}
	// 本页自造的中文常量：按 key 查不到词条，取词函数据 fallback 原样返回（中英界面都是它）。
	for _, lang := range []string{"zh-CN", "en-US"} {
		if got := customerPageFacingText(customerFacingLangCtx(t, lang))(customerUnavailableText); got != customerUnavailableText {
			t.Errorf("%s：自造中文文案应原样返回，实际 %q", lang, got)
		}
	}
}

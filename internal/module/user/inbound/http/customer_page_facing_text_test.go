package userhttp

// customer_page_facing_text_test.go — 后台客户页「命中白名单后页面显示的是译文、不是裸 key」的回归。
//
// 根因（2026-09，与 order / return 两页同源）：userenums.UserFacingMessages 里存的是 i18n
// **item_key**（user.msg.customerDisabled / user.err.userNotFound …），而 customers.html 与
// customer_detail.html 里的 {{.Err}} 是**直接渲染**的文本、不经过 pkg/response 的
// translate —— 只放行 key 的话，运营看到的就是「user.err.userNotFound」。
//
// 修法照 return_page_query.go 的样板：**判定只有一份**（customerFacingText），
// 页面出口多一层取词（customerPageFacingText）。
//
// 写动作的成功 / 失败文案现在由 shell.RenderJump 渲染（见 customerJumpDone / customerJumpFail），
// 本出口只剩「页面取数失败」（customerFacingError）这一条消费路径。
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

	// 页面出口（取数失败提示条）：同一份判定 + 按当前语言取词。
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

// TestCustomerPageFacingTextKeepsNonWhitelistOut 非白名单原文与自造文案都不受影响。
func TestCustomerPageFacingTextKeepsNonWhitelistOut(t *testing.T) {
	i18n.InjectForTest(customerFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	// 未命中白名单：落空串，由调用方换归口文案（绝不把数据库原文带上页面）。
	if got := customerPageFacingText(customerFacingLangCtx(t, "en-US"))(`pq: relation "users" does not exist`); got != "" {
		t.Errorf("非白名单原文应落空串，实际 %q", got)
	}
	// 本页自造的文案（如「能力未装配」）**不再经 ?err= 回显**，而是直接经 userLabelOf 渲染，
	// 所以它们不该被这条白名单出口放行 —— 放行反而说明读侧判定没有删干净。
	for _, lang := range []string{"zh-CN", "en-US"} {
		if got := customerPageFacingText(customerFacingLangCtx(t, lang))(customerUnavailableLabel.fallback); got != "" {
			t.Errorf("%s：自造文案不应经白名单出口放行，实际 %q", lang, got)
		}
	}
}

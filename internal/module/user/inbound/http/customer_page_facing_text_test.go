package userhttp

// customer_page_facing_text_test.go — 后台客户页「页面出口给的是译文、不是裸 key」的回归。
//
// 判据在本批改成了**形态**（pkg/response.IsBusinessError）：userenums 的值本身就是
// i18n item_key，也是 `模块.类别.语义` 形态 —— 所以模块侧那份白名单
// （userenums.UserFacingMessages / customerFacingText / customerPageFacingText）整批删除。
//
// 这里钉住两件事：
//   · 业务错误 → 取当前语言的译文（不是裸 key）；
//   · 基础设施错误（数据库原文）→ 归口文案，原文不进页面（只进日志）。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条（英文值带 EN- 前缀，与中文毫无相似度）：
// 不建库、不起装配，任何一处没按当前语言取词都会立刻暴露。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go_wp/internal/adminui"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// customerFacingInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var customerFacingInjectedEntries = map[string]map[string]string{
	userenums.MsgCustomerDisabled:      {"zh-CN": "账号已停用", "en-US": "EN-CUSTOMER-DISABLED"},
	userenums.ErrCustomerStatusInvalid: {"zh-CN": "账号状态取值不合法", "en-US": "EN-STATUS-INVALID"},
	adminui.MsgInternalError:           {"zh-CN": "系统内部错误，请稍后重试", "en-US": "EN-INTERNAL"},
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

// TestCustomerErrMsgCarriesFactThenTranslates 页面出口给的是**事实**（key + 兜底），
// 取词后按当前语言出译文。
func TestCustomerErrMsgCarriesFactThenTranslates(t *testing.T) {
	i18n.InjectForTest(customerFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	// 事实形态：key 就是错误的原文（enums 常量值即 item_key），兜底同 key。
	fact := customerErrMsg(customerFacingLangCtx(t, "zh-CN"), errors.New(userenums.ErrCustomerStatusInvalid))
	if fact.Key != userenums.ErrCustomerStatusInvalid || fact.Fallback != userenums.ErrCustomerStatusInvalid {
		t.Fatalf("结论应原样带出 key 与兜底，实际 %+v", fact)
	}
	if fact.Empty() {
		t.Fatal("命中业务错误时结论不应为空 —— 空结论会让模板的 HasErr 判成假、提示条整条消失")
	}

	// 成品：按当前语言取词（模板侧 .["t"](ErrKey, ErrFallback) 走的是同一条链）。
	if got := customerErrText(customerFacingLangCtx(t, "zh-CN"), errors.New(userenums.ErrCustomerStatusInvalid)); got != "账号状态取值不合法" {
		t.Errorf("zh-CN 页面出口应给译文，实际 %q（裸 key 说明少了取词那一层）", got)
	}
	if got := customerErrText(customerFacingLangCtx(t, "en-US"), errors.New(userenums.ErrCustomerStatusInvalid)); got != "EN-STATUS-INVALID" {
		t.Errorf("en-US 页面出口应给英文译文，实际 %q", got)
	}

	// 跨模块成品（membershipcontract.FacingTexter 那类）：Key 为空、Fallback 即成品。
	// MsgText 在 Key=="" 时原样返回 Fallback —— 这是合法的第二种形态。
	text := customerMsg{Fallback: "会员等级暂时读不出来"}
	if got := adminui.MsgText(customerFacingLangCtx(t, "en-US"), text); got != "会员等级暂时读不出来" {
		t.Errorf("成品结论应原样输出，实际 %q", got)
	}
}

// TestCustomerErrMsgFallsBackToInternalFact 非业务错误（数据库原文）落归口文案，原文不进页面。
func TestCustomerErrMsgFallsBackToInternalFact(t *testing.T) {
	i18n.InjectForTest(customerFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	c := customerFacingLangCtx(t, "en-US")
	raw := `pq: relation "users" does not exist`

	fact := customerErrMsg(c, errors.New(raw))
	if fact.Key != adminui.MsgInternalError {
		t.Errorf("非业务错误应落归口 key，实际 %q", fact.Key)
	}
	if got := customerErrText(c, errors.New(raw)); got != "EN-INTERNAL" {
		t.Errorf("归口文案应按当前语言取词，实际 %q", got)
	}
	if got := customerErrText(c, errors.New(raw)); got == raw {
		t.Error("数据库原文不得进页面（只进日志）")
	}

	// nil error → 空结论（模板据此不渲染提示条）。
	if msg := customerErrMsg(c, nil); !msg.Empty() {
		t.Errorf("nil error 应给空结论，实际 %+v", msg)
	}
	if got := customerErrText(c, nil); got != "" {
		t.Errorf("nil error 的成品应为空串，实际 %q", got)
	}
}

// TestCustomerNoticeResolvesEnumKeyByLanguage 写动作回执按**系统语言**解析枚举 key。
//
// 回执走的是 userenums 的 msg key（业务侧的唯一真源），页面 UI 文案走模板里的
// 展示词条 —— 两条链各自取词、互不代填：在这里写死中文就等于英文界面恒中文，
// 而把模板词条塞进回执则会让「业务事实」与「UI 措辞」绑成一份（改一处动两处）。
//
// 词条缺失时回落 customerNoticeFallback 的中文兜底（而不是把 key 摆给运营看），
// 这一支由第二个断言钉住。
func TestCustomerNoticeResolvesEnumKeyByLanguage(t *testing.T) {
	key := userenums.MsgCustomerEnabled
	i18n.InjectForTest(map[string]map[string]string{
		key: {"zh-CN": "账号已启用", "en-US": "EN-CUSTOMER-ENABLED"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	if got := customerNotice(customerFacingLangCtx(t, "zh-CN"), key); got != "账号已启用" {
		t.Errorf("zh-CN 回执应给译文，实际 %q", got)
	}
	if got := customerNotice(customerFacingLangCtx(t, "en-US"), key); got != "EN-CUSTOMER-ENABLED" {
		t.Errorf("en-US 回执应给英文译文，实际 %q（裸 key 或恒中文说明少了取词那一层）", got)
	}

	// 未注入词条的 key：中文兜底生效，绝不显示裸 key。
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	if got := customerNotice(customerFacingLangCtx(t, "en-US"), userenums.MsgCustomerDisabled); got != customerNoticeFallback[userenums.MsgCustomerDisabled] {
		t.Errorf("词条缺失时应落中文兜底，实际 %q", got)
	}
}

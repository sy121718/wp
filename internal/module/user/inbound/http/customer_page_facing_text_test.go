package userhttp

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

var customerFacingInjectedEntries = map[string]map[string]string{
	userenums.MsgCustomerDisabled:      {"zh-CN": "账号已停用", "en-US": "EN-CUSTOMER-DISABLED"},
	userenums.ErrCustomerStatusInvalid: {"zh-CN": "账号状态取值不合法", "en-US": "EN-STATUS-INVALID"},
	adminui.MsgInternalError:           {"zh-CN": "系统内部错误，请稍后重试", "en-US": "EN-INTERNAL"},
}

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

func TestCustomerErrMsgCarriesFactThenTranslates(t *testing.T) {
	i18n.InjectForTest(customerFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	fact := customerErrMsg(customerFacingLangCtx(t, "zh-CN"), errors.New(userenums.ErrCustomerStatusInvalid))
	if fact.Key != userenums.ErrCustomerStatusInvalid || fact.Fallback != userenums.ErrCustomerStatusInvalid {
		t.Fatalf("结论应原样带出 key 与兜底，实际 %+v", fact)
	}
	if fact.Empty() {
		t.Fatal("命中业务错误时结论不应为空 —— 空结论会让模板的 HasErr 判成假、提示条整条消失")
	}

	if got := customerErrText(customerFacingLangCtx(t, "zh-CN"), errors.New(userenums.ErrCustomerStatusInvalid)); got != "账号状态取值不合法" {
		t.Errorf("zh-CN 页面出口应给译文，实际 %q（裸 key 说明少了取词那一层）", got)
	}
	if got := customerErrText(customerFacingLangCtx(t, "en-US"), errors.New(userenums.ErrCustomerStatusInvalid)); got != "EN-STATUS-INVALID" {
		t.Errorf("en-US 页面出口应给英文译文，实际 %q", got)
	}

	text := customerMsg{Fallback: "会员等级暂时读不出来"}
	if got := adminui.MsgText(customerFacingLangCtx(t, "en-US"), text); got != "会员等级暂时读不出来" {
		t.Errorf("成品结论应原样输出，实际 %q", got)
	}
}

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

	if msg := customerErrMsg(c, nil); !msg.Empty() {
		t.Errorf("nil error 应给空结论，实际 %+v", msg)
	}
	if got := customerErrText(c, nil); got != "" {
		t.Errorf("nil error 的成品应为空串，实际 %q", got)
	}
}

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

	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	if got := customerNotice(customerFacingLangCtx(t, "en-US"), userenums.MsgCustomerDisabled); got != customerNoticeFallback[userenums.MsgCustomerDisabled] {
		t.Errorf("词条缺失时应落中文兜底，实际 %q", got)
	}
}

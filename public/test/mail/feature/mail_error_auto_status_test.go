package feature

// mail_error_auto_status_test.go — mail 的 JSON 出口（response.ErrorAuto）在两档输入上的状态码。
//
// 为什么补这一条：pkg/response/error_auto_coverage_ledger_test.go 的台账按**目录**代理判定
// 「这个模块的测试里有没有错误状态码断言」，而 mail 此前躺在 stateAssertionGapBaseline 里。
// 本批新增的访客面用例（tracking_text_test.go）让目录满足了那条代理条件，但它断言的是
// **访客端点**（/_t/*）的 400，不是 ErrorAuto 出口的判据 —— 账本要守的恰恰是后者：
//
//	· 业务错误（enums key 形态）        → 400 + 可展示文案（**不能**回 500 把业务错误说成系统故障）；
//	· 基础设施错误（SQLSTATE / relation）→ 500 + 通用文案，且**不泄漏内部原文**。
//
// 基线里删掉 mail 这一行之前先把真正的判据补上，否则就是拿「目录里有别的状态码断言」
// 冒充「ErrorAuto 出口有断言」（账本注释里那句「过期项会掩盖其实已经修好了这个事实」
// 的反面：没过期的项被当成已修好）。
//
// 用假 service（嵌入 nil 接口 + 覆盖一个方法）而不是真实库：真实 PG 很难**稳定**造出
// 「驱动原文上抛」这一档（要真把表删掉），而它正是判据最危险的漏档方向 ——
// 与 admin_err_reverse_defect_test.go 用假 service 的理由相同。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailhttp "go_wp/internal/module/mail/inbound/http"
)

// errAutoFakeSvc 只覆盖被调用的那一个方法，其余方法保持嵌入的 nil 接口
// （本用例只走 /automation/list 这一条路，其余方法被调用即 panic，是刻意的 fail-fast）。
type errAutoFakeSvc struct {
	mailcontract.MailService
	err error
}

func (f errAutoFakeSvc) ListAutomations(context.Context, *maildto.AutomationListReq) (*maildto.AutomationListResp, error) {
	return nil, f.err
}

// TestMailErrorAutoStatusCode ErrorAuto 出口的两档输入各有正确的状态码与文案。
func TestMailErrorAutoStatusCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 内部错误的通用文案（pkg/response 的 msgInternalError：硬编码、不翻译、不携带原文）。
	const internalText = "服务器内部错误，请稍后重试"
	// 内部细节指纹：驱动原文里出现、通用文案里一定不出现。
	leakTokens := []string{"SQLSTATE", `relation "`, "mail_automations"}

	cases := []struct {
		name     string
		err      error
		wantCode int
		// wantText 是**允许**的文案形态：词条命中时是译文，i18n 组件未初始化时
		// pkg/i18n 落到 key 本身（测试进程不初始化组件，两种都算对）。
		wantText []string
	}{
		{
			name:     "业务错误回 400 且文案可展示",
			err:      errors.New(mailenums.ErrAutomationNotFound),
			wantCode: http.StatusBadRequest,
			wantText: []string{mailenums.ErrAutomationNotFound, "自动化流程不存在"},
		},
		{
			name:     "基础设施错误回 500 且不泄漏原文",
			err:      errors.New(`ERROR: relation "mail_automations" does not exist (SQLSTATE 42P01)`),
			wantCode: http.StatusInternalServerError,
			wantText: []string{internalText},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/api/mail/automation/list", mailhttp.NewHandle(errAutoFakeSvc{err: tc.err}).AutomationList)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/mail/automation/list", nil))

			if recorder.Code != tc.wantCode {
				t.Errorf("应 %d，实际 %d：%s", tc.wantCode, recorder.Code, mailHead(recorder.Body.String()))
			}
			body := recorder.Body.String()
			matched := false
			for _, want := range tc.wantText {
				if strings.Contains(body, want) {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("响应文案不在允许集 %v 内：%s", tc.wantText, mailHead(body))
			}
			if tc.wantCode == http.StatusInternalServerError {
				for _, tok := range leakTokens {
					if strings.Contains(body, tok) {
						t.Errorf("500 响应泄漏内部原文 %q：%s", tok, mailHead(body))
					}
				}
			}
		})
	}
}

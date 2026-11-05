package feature

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
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
)

type errAutoFakeSvc struct {
	mailcontract.MailService
	err error
}

func (f errAutoFakeSvc) ListAutomations(context.Context, *maildto.AutomationListReq) (*maildto.AutomationListResp, error) {
	return nil, f.err
}

func TestMailErrorAutoStatusCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 归口文案只有一处定义：pkg/response 的 InternalFallback。
	const internalText = response.InternalFallback

	leakTokens := []string{"SQLSTATE", `relation "`, "mail_automations"}

	cases := []struct {
		name     string
		err      error
		wantCode int

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
			if tc.name == "业务错误回 400 且文案可展示" {
				i18n.InjectForTest(map[string]map[string]string{
					mailenums.ErrAutomationNotFound: {"zh-CN": "自动化流程不存在"},
				}, nil)
				t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
			}
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

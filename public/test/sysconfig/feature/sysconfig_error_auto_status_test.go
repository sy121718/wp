package feature

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	sysconfighttp "go_wp/internal/module/sysconfig/inbound/http"
)

type errAutoFakeSvc struct {
	sysconfigcontract.Service
	err error
}

func (f errAutoFakeSvc) ListGroups(context.Context) ([]sysconfigcontract.Group, error) {
	return nil, f.err
}

func (f errAutoFakeSvc) ListDictOptions(context.Context, string) ([]sysconfigdto.DictOption, error) {
	return nil, nil
}

func (f errAutoFakeSvc) ListCountryOptions(context.Context, string) ([]sysconfigdto.CountryOption, error) {
	return nil, nil
}

func TestSysConfigErrorAutoStatusCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const internalText = "服务器内部错误，请稍后重试"

	leakTokens := []string{"SQLSTATE", `relation "`, "sys_configs"}

	cases := []struct {
		name     string
		err      error
		wantCode int
		wantText []string
	}{
		{
			name:     "业务错误回 400 且文案可展示",
			err:      sysconfigenums.Err(sysconfigenums.ErrGroupNotFound),
			wantCode: http.StatusBadRequest,
			wantText: []string{"配置分组不存在"},
		},
		{
			name:     "基础设施错误回 500 且不泄漏原文",
			err:      errors.New(`ERROR: relation "sys_configs" does not exist (SQLSTATE 42P01)`),
			wantCode: http.StatusInternalServerError,
			wantText: []string{internalText},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			handle := sysconfighttp.NewAdminHandle(errAutoFakeSvc{err: tc.err})
			router.GET("/api/sysconfig/get", handle.APIList)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/sysconfig/get", nil))

			if recorder.Code != tc.wantCode {
				t.Errorf("应 %d，实际 %d：%s", tc.wantCode, recorder.Code, head(recorder.Body.String()))
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
				t.Errorf("响应文案不在允许集 %v 内：%s", tc.wantText, head(body))
			}
			if tc.wantCode == http.StatusInternalServerError {
				for _, tok := range leakTokens {
					if strings.Contains(body, tok) {
						t.Errorf("500 响应泄漏内部原文 %q：%s", tok, head(body))
					}
				}
			}
		})
	}
}

func head(body string) string {
	const max = 200
	if len(body) <= max {
		return body
	}
	return body[:max] + "…"
}

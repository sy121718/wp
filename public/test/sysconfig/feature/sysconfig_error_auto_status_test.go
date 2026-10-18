package feature

// sysconfig_error_auto_status_test.go — sysconfig 的 JSON 出口（response.ErrorAuto）的状态码与文案。
//
// 为什么补这一条：pkg/response/error_auto_coverage_ledger_test.go 按**目录**代理判定
// 「这个模块的测试里有没有错误状态码断言」，而 sysconfig 此前连 public/test/sysconfig 都没有，
// 于是它的业务错误被判成内部错误时不会有任何测试变红。本批把 APIList 的出口从
// 「裸 ErrorWithMessage(500, 归口 key)」换成 ErrorAuto 之后，这条缺口就变得可观测：
//
//	· 业务错误（enums key 形态）        → 400 + 可展示文案（**不能**回 500 把业务错误说成系统故障）；
//	· 基础设施错误（SQLSTATE / relation）→ 500 + 通用文案，且**不泄漏内部原文**。
//
// 用假 service（嵌入 nil 接口 + 覆盖被调用的方法）：真实 PG 很难稳定造出「驱动原文上抛」这一档
// （要真把表删掉），而它正是判据最危险的漏档方向 —— 与 mail 的同名测试同一个理由。

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

// errAutoFakeSvc 只覆盖被调用的那一个方法；其余方法保持嵌入的 nil 接口（被调用即 panic，
// 刻意的 fail-fast）。两个字典读取口必须实现：NewAdminHandle 会做类型断言，缺了直接 panic。
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

// TestSysConfigErrorAutoStatusCode ErrorAuto 出口的两档输入各有正确的状态码与文案。
func TestSysConfigErrorAutoStatusCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 内部错误的通用文案（pkg/response 的 msgInternalError：硬编码、不翻译、不携带原文）。
	const internalText = "服务器内部错误，请稍后重试"
	// 内部细节指纹：驱动原文里出现、通用文案里一定不出现。
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

// head 截断响应体，让失败信息保持可读。
func head(body string) string {
	const max = 200
	if len(body) <= max {
		return body
	}
	return body[:max] + "…"
}

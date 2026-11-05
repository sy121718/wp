package response_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	text "go_wp/pkg/view/jet"
)

func newPageExitContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	return c, recorder
}

func TestBadRequestForPageHidesRawError(t *testing.T) {
	c, recorder := newPageExitContext("/admin/roles/create")
	secret := `pq: relation "sys_roles" does not exist (42P01)`
	response.BadRequestForPage(c, "roles", errors.New(secret))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("参数错误出口应是 400，got %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, text.InternalFallback) {
		t.Fatalf("响应体应带归口文案（%s），got body=%s", text.InternalFallback, body)
	}
	if strings.Contains(body, secret) || strings.Contains(body, "42P01") || strings.Contains(body, "sys_roles") {
		t.Fatalf("原始错误泄漏进了响应体：%s", body)
	}
}

func TestInternalErrorForPageIsInternalExit(t *testing.T) {
	c, recorder := newPageExitContext("/admin/coupons/edit")
	secret := `pq: relation "order_coupons" does not exist (42P01)`
	response.InternalErrorForPage(c, "coupon-edit-form", errors.New(secret))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("局部内部错误出口应是 500，got %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, text.InternalFallback) {
		t.Fatalf("响应体应带归口文案（%s），got body=%s", text.InternalFallback, body)
	}
	if strings.Contains(body, secret) || strings.Contains(body, "42P01") || strings.Contains(body, "order_coupons") {
		t.Fatalf("原始错误泄漏进了响应体：%s", body)
	}

	c2, recorder2 := newPageExitContext("/admin/coupons/edit")
	response.InternalErrorForPage(c2, "coupon-edit-form", nil)
	if recorder2.Code != http.StatusInternalServerError {
		t.Fatalf("nil err 也应写出 500，got %d", recorder2.Code)
	}
}

func TestInternalErrorForPageKeepsProtocol(t *testing.T) {
	wantCtx, wantRecorder := newPageExitContext("/admin/coupons/edit")
	response.ErrorWithMessage(wantCtx, http.StatusInternalServerError, response.InternalKey)

	gotCtx, gotRecorder := newPageExitContext("/admin/coupons/edit")
	response.InternalErrorForPage(gotCtx, "coupon-edit-form", errors.New("内部细节：表 order_coupons"))

	if gotRecorder.Code != wantRecorder.Code {
		t.Fatalf("状态码与统一出口不一致：got %d want %d", gotRecorder.Code, wantRecorder.Code)
	}
	if got, want := gotRecorder.Header().Get("Content-Type"), wantRecorder.Header().Get("Content-Type"); got != want {
		t.Fatalf("Content-Type 不一致：got %q want %q", got, want)
	}
	if got, want := gotRecorder.Body.String(), wantRecorder.Body.String(); got != want {
		t.Fatalf("响应体必须逐字节相同：\ngot  %q\nwant %q", got, want)
	}
}

func TestInternalTextFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		"uiInternalErr": {"zh-CN": "系统内部错误，请稍后重试", "en-US": "Internal error, please retry later"},
	}, nil)
	defer i18n.InjectForTest(nil, nil)

	if c, _ := newPageExitContext("/admin/roles?lang=en-US"); response.InternalText(c) != "Internal error, please retry later" {
		t.Fatalf("en-US 应取注入的英文词条，got %q", response.InternalText(c))
	}
	if c, _ := newPageExitContext("/admin/roles?lang=zh-CN"); response.InternalText(c) != "系统内部错误，请稍后重试" {
		t.Fatalf("zh-CN 应取注入的中文词条，got %q", response.InternalText(c))
	}

	i18n.InjectForTest(nil, nil)
	for _, path := range []string{"/admin/roles?lang=zh-CN", "/admin/roles?lang=en-US"} {
		c, _ := newPageExitContext(path)
		got := response.InternalText(c)
		if got != "系统内部错误，请稍后重试" {
			t.Fatalf("缺词条时应回落中文兜底，got %q（path=%s）", got, path)
		}
		if got == response.InternalKey {
			t.Fatalf("缺词条时回落成了裸 key：%q", got)
		}
	}
}

func TestInternalKeyIsTheKeyLiteral(t *testing.T) {
	if response.InternalKey != "uiInternalErr" {
		t.Fatalf("InternalKey 必须是 key 字面量 %q，got %q", "uiInternalErr", response.InternalKey)
	}
}

package orderhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
)

type couponExitProjectsStub struct {
	projectcontract.ProjectService
}

func (couponExitProjectsStub) Exists(context.Context, string) (bool, error) {
	return false, errors.New(`pq: relation "sys_projects" does not exist (42P01)`)
}

func couponExitContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	return c, recorder
}

func TestCouponEditFormFailureIsInternalJSONFragment(t *testing.T) {
	const path = "/admin/coupons/edit-form?id=7&project=p-1"

	wantCtx, wantRecorder := couponExitContext(path)
	response.ErrorWithMessage(wantCtx, http.StatusInternalServerError, response.InternalKey)

	c, recorder := couponExitContext(path)
	NewCouponPageHandle(nil, couponExitProjectsStub{}).CouponEditForm(c)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("片段取数失败应是 500，got %d", recorder.Code)
	}
	if got, want := recorder.Header().Get("Cache-Control"), "no-store"; got != want {
		t.Fatalf("片段出口的 Cache-Control 不许被迁家改动：got %q want %q", got, want)
	}
	if got, want := recorder.Body.String(), wantRecorder.Body.String(); got != want {
		t.Fatalf("响应体必须与 pkg/response 的 500 出口逐字节相同：\ngot  %q\nwant %q", got, want)
	}
	if body := recorder.Body.String(); strings.Contains(body, "42P01") || strings.Contains(body, "sys_projects") {
		t.Fatalf("原始错误泄漏进了片段响应体：%s", body)
	}
}

// TestCouponEditFormBadParamsStaySeparate 钉参数非法档的出口形状。
//
// 这一档原来是裸 c.Status(400) 空 body，页面 GET 失败时前端拿不到 message；
// 现在统一交 JSON 体（P1-a），所以断言从「不写响应体」改成「code 与 message
// 都对得上词库译文」。
func TestCouponEditFormBadParamsStaySeparate(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		enums.ErrInvalidParams: {"zh-CN": "请求参数错误", "en-US": "Invalid request parameters"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	for _, path := range []string{
		"/admin/coupons/edit-form?id=abc&project=p-1",
		"/admin/coupons/edit-form?id=0&project=p-1",
		"/admin/coupons/edit-form?id=7&project=",
		"/admin/coupons/edit-form?id=7&project=p-1&extra=%20",
	} {
		c, recorder := couponExitContext(path)
		NewCouponPageHandle(nil, couponExitProjectsStub{}).CouponEditForm(c)

		if path == "/admin/coupons/edit-form?id=7&project=p-1&extra=%20" {

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("多余查询键不应改变分档：got %d（path=%s）", recorder.Code, path)
			}
			continue
		}
		if got := recorder.Code; got != http.StatusBadRequest {
			t.Fatalf("参数非法应是 400，got %d（path=%s）", got, path)
		}
		body := recorder.Body.String()
		if body == "" {
			t.Fatalf("参数非法档必须写 JSON 体（页面 GET 失败也要有 message），path=%s", path)
		}
		payload := struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}{}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("响应体不是 JSON：%v（body=%q，path=%s）", err, body, path)
		}
		if payload.Code != http.StatusBadRequest {
			t.Fatalf("code = %d，want 400（path=%s）", payload.Code, path)
		}
		if payload.Message != "请求参数错误" {
			t.Fatalf("message = %q，want 词库译文「请求参数错误」（path=%s）", payload.Message, path)
		}
	}
}

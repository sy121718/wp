package orderhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	projectcontract "go_wp/internal/module/project/contract"
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
	response.ErrorWithMessage(wantCtx, http.StatusInternalServerError, adminui.MsgInternalError)

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

func TestCouponEditFormBadParamsStaySeparate(t *testing.T) {
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
		if got := c.Writer.Status(); got != http.StatusBadRequest {
			t.Fatalf("参数非法应是裸 400，got %d（path=%s）", got, path)
		}
		if body := recorder.Body.String(); body != "" {
			t.Fatalf("参数非法档不应写响应体，got %q（path=%s）", body, path)
		}
	}
}

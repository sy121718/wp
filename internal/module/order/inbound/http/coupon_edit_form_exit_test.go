package orderhttp

// coupon_edit_form_exit_test.go — 优惠码编辑抽屉（片段端点）失败档的**逐字节**契约（task-114）。
//
// 为什么单独钉它：coupon 的编辑抽屉是拿不到页壳的片段端点（浏览器把它塞进抽屉节点），
// 取数失败时它对外只有一个 JSON 出口 —— 原 `shell.PageError`，task-114 起是
// `adminui.PageErrorInternal`。函数是原样搬家的（响应形状不变），但**搬家本身也需要证据**：
// 这个端点没有别的测试碰过它的失败档，而它恰好是这次迁家的两个真实调用点。
//
// 判据分三层，缺一层都会让「改坏了却没人知道」成为可能：
//  1. 状态码 500（不是 400/200：前端据此分「你填错了」与「系统出问题了」）；
//  2. 响应体与 `pkg/response.ErrorWithMessage(500, MsgInternalError)` **逐字节相同**
//     —— 宽松成 contains(key) 的话，随手加个字段或换句兜底文案都不会被发现；
//  3. 片段出口的 `Cache-Control: no-store` 仍在（迁家不该顺手改掉端点既有的头），
//     且原始错误（表名 / SQLSTATE）不得出现在响应体里。
//
// 300× 里没有真库、没有 bootstrap：把 projects 换成「Exists 直接报错」的桩，
// 就停在搬家后的那个调用点（`coupon_page.go` 的 `adminui.PageErrorInternal(c, "coupon-edit-form", err)`）。

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

// couponExitProjectsStub 只覆写 Exists，其余方法由嵌入的接口给出（nil）。
//
// 嵌入接口而不是手写 25 个空方法：本用例只走「工程存在性查询失败」这一条，
// 其余方法一旦被触达就是 nil panic —— 那是**用例设计错**的响亮失败，不是静默假绿。
type couponExitProjectsStub struct {
	projectcontract.ProjectService
}

func (couponExitProjectsStub) Exists(context.Context, string) (bool, error) {
	return false, errors.New(`pq: relation "sys_projects" does not exist (42P01)`)
}

// couponExitContext 组一个片段请求上下文（出口只读 path / 查询串）。
func couponExitContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	return c, recorder
}

// TestCouponEditFormFailureIsInternalJSONFragment 取数失败 = 500 + 通用文案片段，且与
// pkg/response 的 500 出口逐字节相同。
func TestCouponEditFormFailureIsInternalJSONFragment(t *testing.T) {
	const path = "/admin/coupons/edit-form?id=7&project=p-1"

	// 参考体：同一路径下 pkg/response 的 500 出口（迁家前的 shell.PageError 就是它）。
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

// TestCouponEditFormBadParamsStaySeparate 参数档与内部错误档不许混：非法 id/project 仍是
// **裸 400 空体**（既有行为，迁家不碰），不是 500，也不多写一句文案。
func TestCouponEditFormBadParamsStaySeparate(t *testing.T) {
	for _, path := range []string{
		"/admin/coupons/edit-form?id=abc&project=p-1",
		"/admin/coupons/edit-form?id=0&project=p-1",
		"/admin/coupons/edit-form?id=7&project=",
		"/admin/coupons/edit-form?id=7&project=p-1&extra=%20", // 参数齐、只是多个多余键：仍走正常档
	} {
		c, recorder := couponExitContext(path)
		NewCouponPageHandle(nil, couponExitProjectsStub{}).CouponEditForm(c)

		// 裸 400 档**没有响应体**，于是 `httptest.Recorder.Code` 仍是默认的 200 —— gin 把
		// WriteHeader 记在自己的 writermem 上，只有写 body（或显式 WriteHeaderNow）才落到 recorder。
		// 所以这里读 `c.Writer.Status()`：读 recorder.Code 会让这条断言永远「通过」。
		if path == "/admin/coupons/edit-form?id=7&project=p-1&extra=%20" {
			// 多余键不参与判定：这一档应落到内部错误出口（桩的 Exists 报错）。
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

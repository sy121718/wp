package inventoryhttp

// 抽屉表单的失败出口：**4xx + JSON**，不重渲片段。
//
// 判据（改造前后各一条，都在这里）：
//   1. 状态码分档 —— 业务错误 400、其余 500（与整页失败同一个 IsBusinessError）；
//   2. 不渲染任何模板 —— htmx 对 4xx 不 swap，抽屉与用户输入原样保留，所以响应体里
//      只有一句话，没有一个 HTML 字节（重渲那套回填通道已整体删除）。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"

	inventoryenums "go_wp/internal/module/inventory/enums"
	json "go_wp/pkg/jsonx"
)

// inventoryCaptureRender 记录「渲染了哪个模板」：本测试的正面判据是**一次都没渲染**。
type inventoryCaptureRender struct {
	name string
	data any
}

func (r *inventoryCaptureRender) Instance(name string, data any) ginrender.Render {
	r.name, r.data = name, data
	return &hxStubInstance{name: name}
}

func inventoryFormContext(t *testing.T, values url.Values, hx bool) (*gin.Context, *httptest.ResponseRecorder, *inventoryCaptureRender) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(rec)
	view := &inventoryCaptureRender{}
	engine.HTMLRender = view
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/inventory/form", strings.NewReader(values.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if hx {
		c.Request.Header.Set("HX-Request", "true")
	}
	return c, rec, view
}

func TestInventoryDrawerFailureReturnsJSON(t *testing.T) {
	internal := errors.New(`pq: relation "inventory_warehouses" does not exist`)
	tests := []struct {
		name   string
		fail   func(c *gin.Context, err error)
		err    error
		status int
		want   string
	}{
		{"warehouse business", (&inventoryPageHandle{}).warehouseFormFail,
			errors.New(inventoryenums.ErrWarehouseCodeTaken), http.StatusBadRequest,
			inventoryenums.FallbackFor(inventoryenums.ErrWarehouseCodeTaken)},
		{"reason business", (&inventoryPageHandle{}).reasonFormFail,
			errors.New(inventoryenums.ErrReasonCodeTaken), http.StatusBadRequest,
			inventoryenums.FallbackFor(inventoryenums.ErrReasonCodeTaken)},
		{"reason edit business", (&inventoryPageHandle{}).reasonEditFormFail,
			errors.New(inventoryenums.ErrReasonStatusInvalid), http.StatusBadRequest,
			inventoryenums.FallbackFor(inventoryenums.ErrReasonStatusInvalid)},
		{"source business", (&inventorySourcePageHandle{}).sourceFormFail,
			errors.New(inventoryenums.ErrSourceSettleInvalid), http.StatusBadRequest,
			inventoryenums.FallbackFor(inventoryenums.ErrSourceSettleInvalid)},
		{"purchase business", (&inventoryPurchasePageHandle{}).purchaseCreateFail,
			errors.New(inventoryenums.ErrInvalidParam), http.StatusBadRequest,
			inventoryenums.FallbackFor(inventoryenums.ErrInvalidParam)},
		{"warehouse internal", (&inventoryPageHandle{}).warehouseFormFail,
			internal, http.StatusInternalServerError, "系统内部错误"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec, view := inventoryFormContext(t, url.Values{"projectId": {"p1"}}, true)
			tt.fail(c, tt.err)
			c.Writer.WriteHeaderNow()

			if rec.Code != tt.status {
				t.Fatalf("status = %d，want %d", rec.Code, tt.status)
			}
			if view.name != "" {
				t.Fatalf("抽屉失败不该渲染任何模板，实际渲染了 %q", view.name)
			}
			if got := rec.Header().Get("HX-Redirect"); got != "" {
				t.Fatalf("抽屉失败不该整页跳转，HX-Redirect = %q", got)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Fatalf("Content-Type = %q，want application/json", ct)
			}
			var body struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("响应体不是 JSON：%v（%q）", err, rec.Body.String())
			}
			if !strings.Contains(body.Message, tt.want) {
				t.Fatalf("message = %q，want 含 %q", body.Message, tt.want)
			}
			for _, leak := range []string{"pq:", "SQLSTATE", "inventory_warehouses"} {
				if strings.Contains(body.Message, leak) {
					t.Fatalf("内部原文泄漏到对外文案：%q", body.Message)
				}
			}
		})
	}
}

// TestInventoryWriteSuccessRedirects 成功回列表：htmx 走 HX-Redirect，原生走 302 + Location。
//
// 成功不再渲染提示页 —— 列表已经变了，回列表本身就是回执。
func TestInventoryWriteSuccessRedirects(t *testing.T) {
	back := inventoryWarehousesPath + "?project=p1"

	c, rec, view := inventoryFormContext(t, url.Values{"projectId": {"p1"}}, true)
	inventoryDone(c, back)
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != back {
		t.Fatalf("htmx 成功跳转: %d %q", rec.Code, rec.Header().Get("HX-Redirect"))
	}
	if view.name != "" {
		t.Fatalf("成功不该渲染模板，实际 %q", view.name)
	}

	c2, rec2, view2 := inventoryFormContext(t, url.Values{"projectId": {"p1"}}, false)
	inventoryDone(c2, back)
	c2.Writer.WriteHeaderNow()
	if rec2.Code != http.StatusFound || rec2.Header().Get("Location") != back {
		t.Fatalf("原生成功跳转: %d %q", rec2.Code, rec2.Header().Get("Location"))
	}
	if view2.name != "" {
		t.Fatalf("成功不该渲染模板（原生），实际 %q", view2.name)
	}
}

// TestInventoryNativeWriteFailureIsJSON 原生分支的失败也是 JSON（不再有整页提示页）。
func TestInventoryNativeWriteFailureIsJSON(t *testing.T) {
	c, rec, view := inventoryFormContext(t, url.Values{"projectId": {"p1"}, "code": {"bad"}}, false)
	(&inventoryPageHandle{}).warehouseFormFail(c, errors.New(inventoryenums.ErrWarehouseCodeTaken))
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("原生失败状态码 = %d，want 400", rec.Code)
	}
	if view.name != "" {
		t.Fatalf("失败不该渲染模板，实际 %q", view.name)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q，want application/json", ct)
	}
}

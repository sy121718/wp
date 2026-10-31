package inventoryhttp

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
	"go_wp/pkg/response"
)

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

		{"reason edit business", (&inventoryPageHandle{}).reasonEditFormFail,
			inventoryenums.Err(inventoryenums.ErrReasonStatusInvalid), http.StatusBadRequest,
			inventoryenums.FallbackFor(inventoryenums.ErrReasonStatusInvalid)},

		{"warehouse not found", (&inventoryPageHandle{}).warehouseFormFail,
			inventoryenums.Err(inventoryenums.ErrWarehouseNotFound), http.StatusNotFound,
			inventoryenums.FallbackFor(inventoryenums.ErrWarehouseNotFound)},

		{"warehouse code taken", (&inventoryPageHandle{}).warehouseFormFail,
			inventoryenums.Err(inventoryenums.ErrWarehouseCodeTaken), http.StatusConflict,
			inventoryenums.FallbackFor(inventoryenums.ErrWarehouseCodeTaken)},
		{"reason code taken", (&inventoryPageHandle{}).reasonFormFail,
			inventoryenums.Err(inventoryenums.ErrReasonCodeTaken), http.StatusConflict,
			inventoryenums.FallbackFor(inventoryenums.ErrReasonCodeTaken)},

		{"conflict with detail", (&inventoryPageHandle{}).warehouseFormFail,
			inventoryenums.ErrDetail(inventoryenums.ErrWarehouseCodeTaken, "WH1"), http.StatusConflict,
			inventoryenums.FallbackFor(inventoryenums.ErrWarehouseCodeTaken)},
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

func TestInventoryWriteSuccessIsJSON(t *testing.T) {
	for _, hx := range []bool{true, false} {
		c, rec, view := inventoryFormContext(t, url.Values{"projectId": {"p1"}}, hx)
		response.Success(c, nil)
		c.Writer.WriteHeaderNow()
		if rec.Code != http.StatusOK {
			t.Fatalf("hx=%v 成功状态码 = %d，want 200", hx, rec.Code)
		}
		if got := rec.Header().Get("HX-Redirect"); got != "" {
			t.Fatalf("hx=%v 成功不该给跳转地址，HX-Redirect = %q", hx, got)
		}
		if got := rec.Header().Get("Location"); got != "" {
			t.Fatalf("hx=%v 成功不该给 Location = %q", hx, got)
		}
		if view.name != "" {
			t.Fatalf("hx=%v 成功不该渲染模板，实际 %q", hx, view.name)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("hx=%v Content-Type = %q，want application/json", hx, ct)
		}
		var body struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("hx=%v 响应体不是 JSON：%v（%q）", hx, err, rec.Body.String())
		}
		if body.Code != 200 {
			t.Fatalf("hx=%v code = %d，want 200", hx, body.Code)
		}
	}
}

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

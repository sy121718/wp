package inventoryhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	inventoryenums "go_wp/internal/module/inventory/enums"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

// inventoryFormEngine 复刻生产接线：失败中间件、请求取词与 HTML 渲染都必须在场。
func inventoryFormEngine(handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	r.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	r.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	r.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Next()
	})
	r.POST("/admin/inventory/form", pageview.HTMLFragment, handler)
	r.POST("/admin/inventory/reasons/bulk-status", pageview.HTMLFragment, handler)
	return r
}

func TestInventoryDrawerFailureReturnsHTMLFragment(t *testing.T) {
	tests := []struct {
		name   string
		fail   func(c *gin.Context, err error)
		err    error
		status int
	}{
		{"reason edit business", (&inventoryPageHandle{}).reasonEditFormFail,
			inventoryenums.Err(inventoryenums.ErrReasonStatusInvalid), http.StatusBadRequest},
		{"warehouse not found", (&inventoryPageHandle{}).warehouseFormFail,
			inventoryenums.Err(inventoryenums.ErrWarehouseNotFound), http.StatusNotFound},
		{"warehouse code taken", (&inventoryPageHandle{}).warehouseFormFail,
			inventoryenums.Err(inventoryenums.ErrWarehouseCodeTaken), http.StatusConflict},
		{"reason code taken", (&inventoryPageHandle{}).reasonFormFail,
			inventoryenums.Err(inventoryenums.ErrReasonCodeTaken), http.StatusConflict},
		{"warehouse internal", (&inventoryPageHandle{}).warehouseFormFail,
			errors.New(`pq: relation "inventory_warehouses" does not exist`), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/admin/inventory/form", strings.NewReader(url.Values{"projectId": {"p1"}}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			engine := inventoryFormEngine(func(c *gin.Context) { tt.fail(c, tt.err) })
			engine.ServeHTTP(rec, req)

			if rec.Code != tt.status {
				t.Fatalf("status = %d，want %d", rec.Code, tt.status)
			}
			if got := rec.Header().Get("HX-Redirect"); got != "" {
				t.Fatalf("失败不该整页跳转，HX-Redirect = %q", got)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("Content-Type = %q，want text/html", ct)
			}
			body := rec.Body.String()
			if !strings.Contains(body, `data-ui-error`) {
				t.Fatalf("失败应渲染错误片段：%s", body)
			}
			for _, leak := range []string{"pq:", "SQLSTATE", "inventory_warehouses"} {
				if strings.Contains(body, leak) {
					t.Fatalf("内部原文泄漏到对外文案：%q", body)
				}
			}
			if strings.Contains(body, response.InternalFallback+"系统内部错误，请稍后重试") {
				t.Fatalf("归口文案重复：%s", body)
			}
		})
	}
}

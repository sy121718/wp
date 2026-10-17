package producthttp

// product_attr_capture_test.go — 只保留「把渲染 data 截下来」的测试脚手架。
//
// 属性页的失败重渲染出口（attrGroupFormFail / attrValuesFormFail）已随写动作迁移删除，
// 原来放它们的 product_attribute_form_fail_test.go 一并删掉；但 captureRender 与
// newAttrCaptureContext 仍被别的渲染判据用例借用（例如 categoryTreeRows 的徽章判据），
// 所以脚手架留在这里。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"
)

// captureRender 一个只记录「渲染了哪个模板、带什么 data」的渲染器。
type captureRender struct {
	name string
	data any
}

func (r *captureRender) Instance(name string, data any) ginrender.Render {
	r.name, r.data = name, data
	return discardRender{}
}

type discardRender struct{}

func (discardRender) WriteContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

func (discardRender) Render(w http.ResponseWriter) error { return nil }

// capturedData 取回渲染 data（片段 data 一律是 gin.H）。
func capturedData(t *testing.T, cap *captureRender) gin.H {
	t.Helper()
	data, ok := cap.data.(gin.H)
	if !ok {
		t.Fatalf("片段 data 类型 = %T，want gin.H", cap.data)
	}
	return data
}

// newAttrCaptureContext 造一个请求上下文，并把渲染 data 留下。
func newAttrCaptureContext(t *testing.T, hxHeader, body string) (*gin.Context, *httptest.ResponseRecorder, *captureRender) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(rec)
	cap := &captureRender{}
	engine.HTMLRender = cap
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/product-attributes/create", strings.NewReader(body))
	if hxHeader != "" {
		c.Request.Header.Set("HX-Request", hxHeader)
	}
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c, rec, cap
}

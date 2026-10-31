package producthttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"
)

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

func capturedData(t *testing.T, cap *captureRender) gin.H {
	t.Helper()
	data, ok := cap.data.(gin.H)
	if !ok {
		t.Fatalf("片段 data 类型 = %T，want gin.H", cap.data)
	}
	return data
}

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

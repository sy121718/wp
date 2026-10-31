package inventoryhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"
)

type hxStubRender struct{}

func (s *hxStubRender) Instance(name string, _ any) ginrender.Render {
	return &hxStubInstance{name: name}
}

type hxStubInstance struct{ name string }

func (i *hxStubInstance) WriteContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

func (i *hxStubInstance) Render(w http.ResponseWriter) error {

	i.WriteContentType(w)
	_, err := w.Write([]byte("FRAG:" + i.name))
	return err
}

func newHXContext(t *testing.T, hxHeader, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(rec)
	engine.HTMLRender = &hxStubRender{}
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/inventory/warehouses/create", strings.NewReader(body))
	if hxHeader != "" {
		c.Request.Header.Set("HX-Request", hxHeader)
	}
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c, rec
}

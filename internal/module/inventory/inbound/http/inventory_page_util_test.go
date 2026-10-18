package inventoryhttp

// inventory_page_util_test.go — 库存页 HTMX 写表单「分档出口」的最小验证（本批铺设的地基）。
//
// 与 producthttp 侧同款、同判据（两个包刻意各持一份，语义必须逐字一致）：
//   · 带 HX-Request 的请求走片段 / HX-Redirect，不带的一律走原生 302；
//   · 回填快照保序、丢空值、列出即补零键。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"
)

// hxStubRender 片段渲染的替身：只回显「渲染了哪个模板」（片段模板由后续各批自己建，
// 本测试验的是分档与状态码，不是片段长相）。
type hxStubRender struct{}

func (s *hxStubRender) Instance(name string, _ any) ginrender.Render {
	return &hxStubInstance{name: name}
}

type hxStubInstance struct{ name string }

func (i *hxStubInstance) WriteContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

func (i *hxStubInstance) Render(w http.ResponseWriter) error {
	// Content-Type 由渲染实例自己写（gin v1.12 的 Context.Render 不再代劳，
	// 真实 jet 渲染器也在自己的 Render 里调 WriteContentType，这里照它的口径）。
	i.WriteContentType(w)
	_, err := w.Write([]byte("FRAG:" + i.name))
	return err
}

// newHXContext 造一个带（可选）HX-Request 头的请求上下文与记录器。
//
// HTMLRender 设在 `gin.CreateTestContext` 返回的 engine 上：那个 engine 就是 c 内部持有的
// 同一个实例（gin v1.12 的 `Context` 没有导出 `Engine()`，只能从这个返回值拿）。
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


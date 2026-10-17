package producthttp

// product_page_util_test.go — 分档判据的请求构造脚手架（newHXContext）。
//
// 商品写表单已全部迁到 JSON 写基座：失败不再由服务端重渲染片段，于是「回填快照」那组用例
//（formEchoFrom + 归一化口径）随实现一起删除。归一化口径现在在
// static/js/ui/json_write.js，由 public/test/product/json_write.test.cjs 覆盖。
//
// 分档判据（isHXRequest）与出口（shell.RenderJump）的守卫在 product_jump_test.go。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"
)

// hxStubRender 片段渲染的替身：只记录「渲染了哪个模板」并回显模板名。
//
// 本测试验的是回填与分档，不是片段长相 —— 片段模板由后续各批自己建，
// 这里若去渲染真实模板，测试会跟着模板字段漂移（而它并不是本 helper 的契约）。
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
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/products/create", strings.NewReader(body))
	if hxHeader != "" {
		c.Request.Header.Set("HX-Request", hxHeader)
	}
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c, rec
}

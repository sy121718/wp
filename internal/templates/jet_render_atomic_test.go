package templates_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
	"github.com/gin-gonic/gin"

	"go_wp/internal/templates"
	"go_wp/pkg/response"
)

// 本文件的断言口径：
//
//	· 裸渲染器（不经中间件）只负责「不写出半成品」并把失败交回去 ——
//	  写 500 归 FailedRenderMiddleware + 统一 JSON 出口，所以裸路径断言的是
//	  返回 TemplateRenderError 与 body 为空，不是状态码。
//	· 真实路由挂上生产装配里的中间件后，才是「模板失败 → JSON 500」这条链路。
func TestJetRenderFailureDoesNotCommitPartialSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	loader := jet.NewInMemLoader()
	loader.Set("broken.html", "<html>partial-secret{{ missing() }}</html>")
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRenderWithSet(jet.NewSet(loader))
	r.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) {
		response.ErrorStatusAuto(c, "template", err)
	}))
	r.GET("/", func(c *gin.Context) { c.HTML(http.StatusOK, "broken.html", nil) })
	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("模板失败仍返回成功: %d body=%q", res.Code, res.Body.String())
	}
	if ct := res.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("模板失败应走统一 JSON 出口，实际 Content-Type: %q body=%q", ct, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "partial-secret") || strings.Contains(res.Body.String(), "missing") {
		t.Fatalf("错误响应泄漏模板片段: %q", res.Body.String())
	}

	// 裸渲染器：模板加载失败只返回可辨识错误，不自行写响应体。
	missing := httptest.NewRecorder()
	render := templates.NewJetHTMLRenderWithSet(jet.NewSet(loader))
	err := render.Instance("does-not-exist.html", nil).Render(missing)
	if err == nil {
		t.Fatal("模板加载失败必须返回错误")
	}
	if _, ok := templates.AsTemplateRenderError(err); !ok {
		t.Fatalf("模板失败必须是可辨识的 TemplateRenderError，实际: %T %v", err, err)
	}
	if missing.Body.Len() != 0 {
		t.Fatalf("裸渲染器不应写出部分响应体: %q", missing.Body.String())
	}
	if strings.Contains(missing.Body.String(), "does-not-exist") {
		t.Fatal("模板路径不得进入响应")
	}
}

func TestJetRuntimePanicDoesNotCommitPartialHTML(t *testing.T) {
	loader := jet.NewInMemLoader()
	loader.Set("panic.html", `partial-secret{{explode()}}`)
	set := jet.NewSet(loader)
	set.AddGlobal("explode", func() string { panic("private-runtime-secret") })
	recorder := httptest.NewRecorder()
	err := templates.NewJetHTMLRenderWithSet(set).Instance("panic.html", nil).Render(recorder)
	if _, ok := templates.AsTemplateRenderError(err); !ok {
		t.Fatalf("执行期 panic 应转为 TemplateRenderError：%T %v", err, err)
	}
	if recorder.Body.Len() != 0 || recorder.Header().Get("Content-Type") != "" || recorder.Flushed {
		t.Fatalf("执行期 panic 不得提交半截 HTML：%v %q", recorder.Header(), recorder.Body.String())
	}
}

func TestJetRenderSuccessSetsHTMLContentType(t *testing.T) {
	loader := jet.NewInMemLoader()
	loader.Set("ok.html", "<html>完整页面</html>")
	render := templates.NewJetHTMLRenderWithSet(jet.NewSet(loader))
	res := httptest.NewRecorder()
	if err := render.Instance("ok.html", nil).Render(res); err != nil {
		t.Fatal(err)
	}
	if res.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal(res.Header())
	}
	if res.Body.String() != "<html>完整页面</html>" {
		t.Fatal(res.Body.String())
	}
}

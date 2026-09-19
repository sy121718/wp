package templates

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
	"github.com/gin-gonic/gin"
)

func TestJetRenderFailureDoesNotCommitPartialSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	loader := jet.NewInMemLoader()
	loader.Set("broken.html", "<html>partial-secret{{ missing() }}</html>")
	r := gin.New()
	r.HTMLRender = &jetHTMLRender{set: jet.NewSet(loader)}
	r.GET("/", func(c *gin.Context) { c.HTML(http.StatusOK, "broken.html", nil) })
	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("模板失败仍返回成功: %d body=%q", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "partial-secret") || strings.Contains(res.Body.String(), "missing") {
		t.Fatalf("错误响应泄漏模板片段: %q", res.Body.String())
	}
	missing := httptest.NewRecorder()
	render := &jetHTMLRender{set: jet.NewSet(loader)}
	if err := render.Instance("does-not-exist.html", nil).Render(missing); err == nil || missing.Code != http.StatusInternalServerError {
		t.Fatalf("模板加载失败必须受控返回 500：%d %v", missing.Code, err)
	}
	if strings.Contains(missing.Body.String(), "does-not-exist") {
		t.Fatal("模板路径不得进入响应")
	}

}

func TestJetRenderSuccessSetsHTMLContentType(t *testing.T) {
	loader := jet.NewInMemLoader()
	loader.Set("ok.html", "<html>完整页面</html>")
	render := &jetHTMLRender{set: jet.NewSet(loader)}
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

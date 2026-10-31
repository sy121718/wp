package view

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRenderTemplateUsesRequestRenderer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	calls := 0
	renderer := func(c *gin.Context, tpl string, data any) {
		calls++
		if tpl != "admin/product/products.html" || data != "facts" {
			t.Fatalf("出口参数丢失: %s %v", tpl, data)
		}
		c.String(http.StatusOK, "完整页面")
	}
	engine.GET("/", UseRenderer(renderer), func(c *gin.Context) { RenderTemplate(c, "admin/product/products.html", "facts") })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "完整页面" || calls != 1 {
		t.Fatalf("直接模板出口失败: %d %s calls=%d", rec.Code, rec.Body.String(), calls)
	}
}

func TestRenderTemplateRejectsMissingRendererAndNilData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, withRenderer := range []bool{false, true} {
		t.Run(map[bool]string{false: "缺少渲染器", true: "空数据"}[withRenderer], func(t *testing.T) {
			engine := gin.New()
			if withRenderer {
				engine.Use(UseRenderer(func(c *gin.Context, tpl string, data any) { t.Fatal("空数据进入渲染器") }))
			}
			engine.GET("/", func(c *gin.Context) {
				var data any = "facts"
				if withRenderer {
					data = nil
				}
				RenderTemplate(c, "admin/product/products.html", data)
			})
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
				t.Fatalf("失败出口非500 JSON: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

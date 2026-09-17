package producthttp

// product_page_render_test.go — 商品后台页渲染测试的共用辅助。
//
// 从 dashboard/inbound/http/article_page_render_test.go 搬迁：那两个辅助函数
// （layout 数据补齐 + 真实 Jet 渲染一个后台模板）本身与文章无关，商品页渲染测试同样需要。
// 模板根目录相对包目录的层数一致（internal/module/<mod>/inbound/http → internal/templates），
// 因此这里的相对路径与 dashboard 侧逐字相同。

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/templates"
)

// productPageLayoutData 补齐 layout 需要的键（与线上 shell.Prepare 注入的一致）。
func productPageLayoutData(base gin.H) gin.H {
	lang := "zh-CN"
	base["csrf_token"] = "test-token"
	base["lang"] = lang
	base["t"] = templates.TranslateFunc(lang)
	base["langs"] = templates.LanguageOptions(lang)
	base["lang_redirect"] = "/admin/products"
	return base
}

// renderAdminTemplate 用真实 Jet 渲染器渲染一个后台模板并返回 HTML。
func renderAdminTemplate(t *testing.T, name string, data gin.H) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 模板根目录相对包目录：internal/module/product/inbound/http → internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/page", func(c *gin.Context) { c.HTML(http.StatusOK, name, data) })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s 渲染失败，状态 %d", name, rec.Code)
	}
	return rec.Body.String()
}

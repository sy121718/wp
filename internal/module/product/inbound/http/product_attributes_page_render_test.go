package producthttp

import (
	"go_wp/internal/adminui"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
)

func attributesPageRequest(t *testing.T, h *ProductPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)

	engine.Use(func(c *gin.Context) {
		c.Set("button_set", map[string]bool{
			"product.attribute_create": true,
			"product.attribute_update": true,
			"product.attribute_delete": true,
		})
		c.Next()
	})
	engine.GET("/admin/product-attributes", pageview.UseRenderer(adminui.RenderPage), h.ProductAttributesPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestProductAttributesPageSuccessRendersRowsAndDrawers(t *testing.T) {
	stub := &productPageReaderStub{attributes: []*productdto.AttributeResp{{ID: "a1", ProjectID: "p1", Key: "color", Name: "颜色", IsVariation: true, Values: []productdto.AttributeValueResp{{ID: "v1", Label: "红色", Sort: 3, Enabled: true}}}}}
	rec := attributesPageRequest(t, &ProductPageHandle{products: stub}, "/admin/product-attributes?project=p1&keyword=col&variation=1&page=2")
	if rec.Code != 200 {
		t.Fatalf("渲染失败: %d %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"</html>", "颜色", "红色", "tpl-attr-edit-a1", "tpl-attr-values-a1", "tpl-attr-create", "data-attribute-add", "data-write-json=\"values\""} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("缺少%s", want)
		}
	}
	if len(stub.queryReqs) != 1 || stub.queryReqs[0].Keyword != "col" || stub.queryReqs[0].Page != 2 || stub.queryReqs[0].Variation != "1" {
		t.Fatalf("查询参数丢失: %+v", stub.queryReqs)
	}
}
func TestProductAttributesPageUnknownProjectIsBadRequest(t *testing.T) {
	stub := &productPageReaderStub{}
	rec := attributesPageRequest(t, &ProductPageHandle{products: stub}, "/admin/product-attributes?project=missing")
	assertProductJSONFailure(t, "工程不存在", rec, http.StatusBadRequest, "请求参数无效")
	if len(stub.queryReqs) != 0 {
		t.Fatal("无效工程之后仍查属性")
	}
}
func TestProductAttributesPageUsesJSONWriteBase(t *testing.T) {
	stub := &productPageReaderStub{attributes: []*productdto.AttributeResp{{ID: "a1", Name: "颜色"}}}
	rec := attributesPageRequest(t, &ProductPageHandle{products: stub}, "/admin/product-attributes?project=p1")
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	for _, action := range []string{"create", "update", "delete", "bulk-delete", "set-values"} {
		if !strings.Contains(rec.Body.String(), "/api/product/attribute/"+action) {
			t.Errorf("缺少%s", action)
		}
	}
	if strings.Contains(rec.Body.String(), "/admin/product-attributes/value-rows") {
		t.Fatal("行编辑仍回传服务端")
	}
}

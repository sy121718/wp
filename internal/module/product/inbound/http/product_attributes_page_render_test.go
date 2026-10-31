package producthttp

// product_attributes_page_render_test.go — 属性库整页的渲染出口（真 Jet + 真模板）。
//
// 属性页此前由 product_taxonomy_page_render_test.go 的 taxonomyPages 表覆盖（桩直调
// CountAttributes / ListAttributes）。取数下沉之后那两条路径不再经过控制器，所以本文件
// 接住同一族判据，改用**读模型桩**：
//
//	?project=<不存在的 id>    → 400 + no-store
//	正常档                    → 200、完整页壳、行与抽屉表单真的渲染出来、参数转发到读模型
//
// 「读模型没接上 → 500」这一档已从运行期消失（读模型是构造实参，漏接=编译错误，
// 判据见 product_reader_injection_test.go），所以下表里没有它。
//
// 反面判据与 taxonomy 那批同源：只断言「失败档是红色」挡不住「把取数整段跳过」——
// 所以成功档必须断言**该取的数真的取了**（这里体现为读模型收到的入参）。

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

// attributesPageRequest 挂上属性页并 GET 一次（真 Jet 渲染器 + 真模板）。
func attributesPageRequest(t *testing.T, h *ProductPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	// 三个操作按钮码：模板用 isset(.Buttons[...]) 决定是否渲染行内抽屉与页头新建入口
	//（本用例要验的正是「行与抽屉真的渲染出来了」，所以权限位必须给全）。
	// 键名是 internal/middleware/adminctx 的私有约定（button_set），该包不导出键常量
	//（见 page/inbound/http/page_translations_failure_test.go 的同一写法）。
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

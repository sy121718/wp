package producthttp

import (
	"errors"
	"github.com/gin-gonic/gin"
	"go_wp/internal/adminui"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	pageview "go_wp/pkg/view"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func editorRequest(t *testing.T, h *ProductPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { t.Errorf("编辑模板执行失败：%v", err) }))
	engine.GET("/admin/products/edit", pageview.UseRenderer(adminui.RenderPage), h.ProductEditPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
	return rec
}
func TestProductEditConsumesOriginalEntities(t *testing.T) {
	p := productRowForRender()
	price := 12.5
	p.DefaultPrice = &price
	p.DefaultImage = "/uploads/main.png"
	p.Images = []string{"/uploads/a.png", "/uploads/b.png"}
	p.ImageAlts = []string{"第一张", "第二张"}
	p.AttributeIDs = []string{"a1"}
	p.CategoryIDs = []string{"c1"}
	p.PrimaryCategoryID = "c1"
	p.BrandID = "b1"
	p.TagIDs = []string{"t1"}
	s := &productPageReaderStub{product: p, attributes: p.Attributes, categories: []*productdto.CategoryResp{{ID: "c1", Name: "服装"}}, brands: []*productdto.BrandResp{{ID: "b1", Name: "品牌甲"}}, tags: []*productdto.TagResp{{ID: "t1", Name: "精选", Kind: "manual"}}}
	rec := editorRequest(t, &ProductPageHandle{products: s}, "/admin/products/edit?project=p1&product=prod-1")
	if rec.Code != 200 {
		t.Fatalf("编辑页状态 %d: %s", rec.Code, headOf(rec.Body.String()))
	}
	body := rec.Body.String()
	for _, want := range []string{"</html>", "hx-post=\"/api/product/update\"", "name=\"defaultPrice\" value=\"12.5\"", "/uploads/main.png", "第一张", "value=\"a1\" checked", "value=\"c1\" checked", "value=\"b1\" selected", "value=\"t1\" checked", "product-display-data"} {
		if !strings.Contains(body, want) {
			t.Fatalf("编辑页缺少 %q: %s", want, headOf(body))
		}
	}
}
func TestProductEditRejectsIncompleteChoices(t *testing.T) {
	s := &productPageReaderStub{product: productRowForRender(), attributesErr: errors.New("pq: attributes unavailable")}
	rec := editorRequest(t, &ProductPageHandle{products: s}, "/admin/products/edit?project=p1&product=prod-1")
	assertProductJSONFailure(t, "属性候选失败不能当成解绑", rec, 500, "内部错误")
}

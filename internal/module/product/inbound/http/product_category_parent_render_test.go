package producthttp

import (
	"context"
	"github.com/gin-gonic/gin"
	"go_wp/internal/adminui"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type parentSearchStub struct {
	productPageReaderStub
	req *productdto.CatalogTaxonomyReq
}

func (s *parentSearchStub) SearchCategoryParents(_ context.Context, q *productdto.CatalogTaxonomyReq) ([]*productdto.CategoryResp, error) {
	s.req = q
	return []*productdto.CategoryResp{{ID: "cat-1", Name: "上衣"}, {ID: "cat-2", Name: "裤子"}}, nil
}
func TestProductCategoryParentsRendersPickedOptions(t *testing.T) {
	stub := &parentSearchStub{}
	h := &ProductPageHandle{products: stub}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/pick", adminui.BareSlot(ProductCategoryParentsTemplate), h.ProductCategoryParents)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pick?project=p1&keyword=衣&page=2", nil))
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	for _, want := range []string{`data-category-parent-choice="cat-1"`, `data-category-parent-choice="cat-2"`, ">上衣<", ">裤子<"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("缺少%s", want)
		}
	}
	if stub.req == nil || stub.req.ProjectID != "p1" || stub.req.Keyword != "衣" || stub.req.Page != 2 {
		t.Fatalf("查询参数=%+v", stub.req)
	}
}

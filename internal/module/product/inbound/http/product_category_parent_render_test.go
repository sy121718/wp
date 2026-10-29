package producthttp

import (
	"context"
	"github.com/gin-gonic/gin"
	productdto "go_wp/internal/module/product/dto"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
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
func TestProductCategoryParentsReturnsFacts(t *testing.T) {
	stub := &parentSearchStub{}
	h := NewProductHandle(stub)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/pick", h.ProductCategoryParents)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pick?project=p1&keyword=衣&page=2", nil))
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var payload struct {
		Data []*productdto.CategoryResp `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 2 || payload.Data[0].ID != "cat-1" || payload.Data[1].Name != "裤子" {
		t.Fatalf("候选事实缺失：%s", rec.Body.String())
	}
	if stub.req == nil || stub.req.ProjectID != "p1" || stub.req.Keyword != "衣" || stub.req.Page != 2 {
		t.Fatalf("查询参数=%+v", stub.req)
	}
}

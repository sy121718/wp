package producthttp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/html"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

func paginationElement(base string, page, limit int) string {
	return `<a class="pagination-btn" href="` + paginationHref(base, page, limit) + `">` + strconv.Itoa(page) + `</a>`
}

func paginationHref(base string, page, limit int) string {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	raw := base + sep + "page=" + strconv.Itoa(page) + "&limit=" + strconv.Itoa(limit)
	return strings.ReplaceAll(raw, "&", "&amp;")
}

func hasVisiblePagination(t *testing.T, body string) bool {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var visit func(*html.Node) bool
	visit = func(n *html.Node) bool {
		if n.Type == html.ElementNode {
			if n.Data == "template" {
				return false
			}
			for _, a := range n.Attr {
				if a.Key == "hidden" {
					return false
				}
			}
			for _, a := range n.Attr {
				if a.Key == "class" {
					for _, class := range strings.Fields(a.Val) {
						if class == "pagination" {
							return true
						}
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if visit(child) {
				return true
			}
		}
		return false
	}
	return visit(doc)
}

func TestTaxonomyPagesPaginationFactsRenderLinks(t *testing.T) {
	for _, name := range []string{"attributes", "categories", "brands", "tags"} {
		for _, total := range []int64{0, 3, 25} {
			t.Run(name+strconv.Itoa(int(total)), func(t *testing.T) {
				stub := &productPageReaderStub{attributeResult: &productdto.TaxonomyListResp[*productdto.AttributeResp]{Page: 1, Size: 20, Total: total}, categoryResult: &productdto.CategorySearchResp{Page: 1, Size: 20, Total: total}, brandResult: &productdto.TaxonomyListResp[*productdto.BrandResp]{Page: 1, Size: 20, Total: total}, tagResult: &productdto.TaxonomyListResp[*productdto.TagResp]{Page: 1, Size: 20, Total: total}}
				route := "/admin/product-" + name
				rec := productPageRequestFor(t, &ProductPageHandle{products: stub}, route, route+"?project=p1&keyword=pg&variation=1")
				if rec.Code != 200 {
					t.Fatalf("渲染失败: %s", rec.Body.String())
				}
				body := rec.Body.String()
				if total <= 20 {
					if hasVisiblePagination(t, body) {
						t.Fatal("单页仍出现分页条")
					}
					return
				}
				for _, want := range []string{`class="pagination"`, `<span class="pagination-btn is-active">1</span>`, route + "?keyword=pg&amp;project=p1&amp;variation=1&amp;page=2&amp;size=20"} {
					if !strings.Contains(body, want) {
						t.Errorf("缺少%s", want)
					}
				}
			})
		}
	}
}

func TestProductsPagePaginationFactsRenderLinks(t *testing.T) {
	h := &ProductPageHandle{products: &productPageReaderStub{result: &productdto.ProductListResp{Items: []*productdto.ProductResp{}, Total: 25, Page: 1, Size: 20}}}
	rec := productPageRequest(t, h, "/admin/products?project=p1&keyword=abc&status=draft")
	if rec.Code != 200 {
		t.Fatalf("渲染失败: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), strings.ReplaceAll(paginationElement("/admin/products?keyword=abc&project=p1&status=draft", 2, 20), "limit=20", "size=20")) {
		t.Fatal("分页未保留筛选条件")
	}
}
func TestProductsPageFailureAfterTotalIsJSON(t *testing.T) {
	h := &ProductPageHandle{products: &productPageReaderStub{listErr: errors.New("读取商品汇总失败")}}
	assertProductJSONFailure(t, "列表读取失败", productPageRequest(t, h, "/admin/products?project=p1"), 500, "内部错误")
}

type tagHitsStub struct {
	productcontract.ProductService
	resp *productdto.TagProductsListResp
	err  error
}

func (s *tagHitsStub) ListTagProducts(context.Context, *productdto.ListTagProductsReq) (*productdto.TagProductsListResp, error) {
	return s.resp, s.err
}
func TestTagProductsAPIKeepsPaginationFacts(t *testing.T) {
	want := &productdto.TagProductsListResp{TagID: "t1", TagName: "标签", Total: 45, Page: 2, PageSize: 20, Items: []*productdto.TagProductResp{{ID: "p1", Name: "商品"}}}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/product/tag/products", NewProductHandle(&tagHitsStub{resp: want}).ListTagProducts)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/product/tag/products?projectId=p1&id=t1&page=2&size=20", nil))
	var result struct {
		Data *productdto.TagProductsListResp `json:"data"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &result) != nil || !reflect.DeepEqual(result.Data, want) {
		t.Fatalf("分页事实漂移：%d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("HX-Redirect") != "" || rec.Header().Get("Location") != "" {
		t.Fatal("JSON查询不应导航")
	}
}
func TestTagProductsAPIFailureIsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/product/tag/products", NewProductHandle(&tagHitsStub{err: errors.New("SQLSTATE secret_table")}).ListTagProducts)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/product/tag/products?id=t1&projectId=p1", nil))
	assertProductJSONFailure(t, "标签命中读取失败", rec, 500, "内部错误")
	if strings.Contains(rec.Body.String(), "secret_table") {
		t.Fatal("内部错误泄露")
	}
}

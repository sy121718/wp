package producthttp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/templates"
)

func taxonomyJSONWriteRequest(t *testing.T, h *ProductPageHandle, route, target string, mw gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	if mw != nil {
		engine.Use(mw)
	}
	switch route {
	case "/admin/product-categories":
		engine.GET(route, adminui.PageSlot(ProductCategoriesPageTemplate), h.ProductCategoriesPage)
	case "/admin/product-brands":
		engine.GET(route, adminui.PageSlot(ProductBrandsPageTemplate), h.ProductBrandsPage)
	case "/admin/product-tags":
		engine.GET(route, adminui.PageSlot(ProductTagsPageTemplate), h.ProductTagsPage)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func taxonomyAllButtons() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"product.category_create": true, "product.category_update": true, "product.category_delete": true,
			"product.brand_create": true, "product.brand_update": true, "product.brand_delete": true,
			"product.tag_create": true, "product.tag_update": true, "product.tag_delete": true,
		})
	}
}

func assertContainsAll(t *testing.T, html string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Errorf("页面缺少 %q（写入口会退化成原生提交或干脆没有）", want)
		}
	}
}

func assertContainsNone(t *testing.T, html string, gones []string) {
	t.Helper()
	for _, gone := range gones {
		if strings.Contains(html, gone) {
			t.Errorf("页面仍残留 %q：页面 POST 与 JSON 写端点并存，谁接管提交取决于属性顺序", gone)
		}
	}
}

func TestCategoryPageUsesJSONWriteBase(t *testing.T) {
	h := taxonomyHandle(&productPageReaderStub{categories: []*productdto.CategoryResp{{ID: "c1", ProjectID: "p1", Name: "男装"}}})
	rec := taxonomyJSONWriteRequest(t, h, "/admin/product-categories", "/admin/product-categories?project=p1", taxonomyAllButtons())
	if rec.Code != http.StatusOK {
		t.Fatalf("整页 GET 状态码 = %d, want 200", rec.Code)
	}
	html := rec.Body.String()
	assertContainsAll(t, html, []string{
		"data-list-bulk",
		`data-write-url="/api/product/category/bulk-delete"`,
		`data-write-reload`,
		`data-write-url="/api/product/category/delete"`,
		`data-write-reload`,
		`data-write-url="/api/product/category/create"`,
		`data-write="bulk"`,
		`data-write-close="[data-drawer]"`,
		"data-write-status",
		"data-write-host",
	})
	assertContainsNone(t, html, []string{
		`action="/admin/product-categories/bulk-delete"`,
		`action="/admin/product-categories/delete"`,
		`action="/admin/product-categories/create"`,
	})
}

func TestBrandPageUsesJSONWriteBase(t *testing.T) {
	h := taxonomyHandle(&productPageReaderStub{brands: []*productdto.BrandResp{{ID: "b1", Name: "品牌一"}}})
	rec := taxonomyJSONWriteRequest(t, h, "/admin/product-brands", "/admin/product-brands?project=p1", taxonomyAllButtons())
	if rec.Code != http.StatusOK {
		t.Fatalf("整页 GET 状态码 = %d, want 200", rec.Code)
	}
	html := rec.Body.String()
	assertContainsAll(t, html, []string{
		"data-list-bulk",
		`data-write-url="/api/product/brand/bulk-delete"`,
		`data-write-url="/api/product/brand/delete"`,
		`data-write-reload`,
		`data-write-url="/api/product/brand/update"`,
		`data-write-url="/api/product/brand/create"`,
		`data-write="bulk"`,
		`data-write-close="[data-drawer]"`,
		"data-write-status",
		"data-write-host",
	})
	assertContainsNone(t, html, []string{
		`action="/admin/product-brands/bulk-delete"`,
		`action="/admin/product-brands/delete"`,
		`action="/admin/product-brands/create"`,
		`action="/admin/product-brands/update"`,
	})
}

func TestTagPageUsesJSONWriteBase(t *testing.T) {
	h := taxonomyHandle(&productPageReaderStub{tags: []*productdto.TagResp{{ID: "t2", Name: "自动标签", Kind: productenums.TagKindRule, RuleType: productenums.TagRuleNewArrival, RuleValues: productdto.RuleValuesResp{Days: 30, HasDays: true}}}})
	rec := taxonomyJSONWriteRequest(t, h, "/admin/product-tags", "/admin/product-tags?project=p1", taxonomyAllButtons())
	if rec.Code != http.StatusOK {
		t.Fatalf("整页 GET 状态码 = %d, want 200", rec.Code)
	}
	html := rec.Body.String()
	assertContainsAll(t, html, []string{
		"data-list-bulk",
		`data-write-url="/api/product/tag/bulk-delete"`,
		`data-write-url="/api/product/tag/delete"`,
		`data-write-reload`,
		`data-write-url="/api/product/tag/recalc"`,
		`id="tag-recalc-all"`,
		`id="tag-recalc-t2"`,
		`data-write-url="/api/product/tag/create"`,
		`data-write-url="/api/product/tag/update"`,
		`data-write-object=".tag-rule-fields;ruleParams;ruleType;kind=rule"`,
		"data-write-status",
	})
	assertContainsNone(t, html, []string{
		`action="/admin/product-tags/bulk-delete"`,
		`action="/admin/product-tags/delete"`,
		`action="/admin/product-tags/recalc"`,
		`action="/admin/product-tags/create"`,
		`action="/admin/product-tags/update"`,
	})
}

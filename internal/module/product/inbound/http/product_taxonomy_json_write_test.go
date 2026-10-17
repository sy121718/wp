package producthttp

// product_taxonomy_json_write_test.go — 分类 / 品牌 / 标签三页的写动作走 ui/json_write.js 基座。
//
// 判据用**声明**而不是文案：data-write-api / data-write-mode / data-write-extra 是基座读的
// 契约键，少一个就退化成原生提交 —— 而页面 POST 的 action 已经删掉，于是「点了没反应」，
// 不报错、日志干净、控制台也没有异常。负向断言同样必要：旧 action 还在的话，带 data-confirm
// 的按钮在确认后仍会被原生表单接管（ui/confirm.js 摘掉属性重派发，本基座此时也会接管，
// 但两条路径同时存在时谁是权威取决于属性顺序 —— 这种歧义不能留在页面上）。

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	"go_wp/internal/templates"
)

// taxonomyJSONWriteRequest 真 Jet 渲染一页，并可挂一个「注入按钮码」的中间件。
//
// 不注入按钮码时模板里的 {{if canDelete}} 一律为假，整段写入口不渲染 —— 断言会假绿
// （在「页面没有入口」与「入口用了旧动作」两种完全不同的缺陷上同时通过）。
func taxonomyJSONWriteRequest(t *testing.T, h *productPageHandle, route, target string, mw gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	if mw != nil {
		engine.Use(mw)
	}
	switch route {
	case "/admin/product-categories":
		engine.GET(route, h.ProductCategoriesPage)
	case "/admin/product-brands":
		engine.GET(route, h.ProductBrandsPage)
	case "/admin/product-tags":
		engine.GET(route, h.ProductTagsPage)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// taxonomyAllButtons 三页全部写权限：缺一个就有一段入口不渲染。
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
			t.Errorf("分类页缺少 %q（写入口会退化成原生提交或干脆没有）", want)
		}
	}
}

func assertContainsNone(t *testing.T, html string, gones []string) {
	t.Helper()
	for _, gone := range gones {
		if strings.Contains(html, gone) {
			t.Errorf("分类页仍残留 %q：页面 POST 与 JSON 写端点并存，谁接管提交取决于属性顺序", gone)
		}
	}
}

func TestCategoryPageUsesJSONWriteBase(t *testing.T) {
	h := taxonomyHandle(&productPageReaderStub{categoriesView: productCategoriesPageViewStub(nil)})
	rec := taxonomyJSONWriteRequest(t, h, "/admin/product-categories", "/admin/product-categories?project=p1", taxonomyAllButtons())
	if rec.Code != http.StatusOK {
		t.Fatalf("整页 GET 状态码 = %d, want 200", rec.Code)
	}
	html := rec.Body.String()
	assertContainsAll(t, html, []string{
		// 批量删除：bulk 档 + 作用域锚点
		"data-list-bulk",
		`data-write-api="/api/product/category/bulk-delete"`,
		`data-write-mode="bulk"`,
		`data-msg-all-deleted`,
		`data-msg-partial`,
		// 单条删除：single 档 + 多工程下的 projectId
		`data-write-api="/api/product/category/delete"`,
		`data-write-mode="single"`,
		`data-write-extra="projectId"`,
		// 抽屉：整表单档（新建与编辑同一份字段清单）
		`data-write-api="/api/product/category/create"`,
		`data-write-mode="form"`,
		`data-write-number="sort"`,
		// 结果区（失败时才有内容）
		"data-write-status",
		"data-write-host",
	})
	assertContainsNone(t, html, []string{
		`action="/admin/product-categories/bulk-delete"`,
		`action="/admin/product-categories/delete"`,
		`action="/admin/product-categories/create"`,
	})
}

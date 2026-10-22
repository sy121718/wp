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

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/templates"
)

// taxonomyJSONWriteRequest 真 Jet 渲染一页，并可挂一个「注入按钮码」的中间件。
//
// 不注入按钮码时模板里的 {{if canDelete}} 一律为假，整段写入口不渲染 —— 断言会假绿
// （在「页面没有入口」与「入口用了旧动作」两种完全不同的缺陷上同时通过）。
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
	h := taxonomyHandle(&productPageReaderStub{categoriesView: productCategoriesPageViewStub(nil)})
	rec := taxonomyJSONWriteRequest(t, h, "/admin/product-categories", "/admin/product-categories?project=p1", taxonomyAllButtons())
	if rec.Code != http.StatusOK {
		t.Fatalf("整页 GET 状态码 = %d, want 200", rec.Code)
	}
	html := rec.Body.String()
	assertContainsAll(t, html, []string{
		// 批量删除：bulk 档 + 作用域锚点
		"data-list-bulk",
		`hx-post="/api/product/category/bulk-delete"`,
		`data-reload-on-ok`,
		`data-write-flash`,
		// 单条删除：single 档 + 多工程下的 projectId
		`hx-post="/api/product/category/delete"`,
		`data-reload-on-ok`,
		// 抽屉：整表单档（新建与编辑同一份字段清单）。
		//
		// 这一档已从 ui/json_write.js 迁到 htmx（formdata）：端点还是同一个，
		// binding.JSON 按 Content-Type 自适应，所以两边都能进。断言因此换成
		// hx-post + hx-swap="none" —— 判据不变：**写入口必须声明端点**，
		// 少了它按钮会退化成原生提交（页面 POST 的 action 早已删除 ⇒ 点了没反应）。
		`hx-post="/api/product/category/create"`,
		`hx-swap="none"`,
		`data-close-on-ok="[data-drawer]"`,
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

func TestBrandPageUsesJSONWriteBase(t *testing.T) {
	h := taxonomyHandle(&productPageReaderStub{brandsView: productBrandsPageViewStub(nil)})
	rec := taxonomyJSONWriteRequest(t, h, "/admin/product-brands", "/admin/product-brands?project=p1", taxonomyAllButtons())
	if rec.Code != http.StatusOK {
		t.Fatalf("整页 GET 状态码 = %d, want 200", rec.Code)
	}
	html := rec.Body.String()
	assertContainsAll(t, html, []string{
		"data-list-bulk",
		// 批量与单条删除也都迁到 htmx 了。批量在本仓是**整批一个事务、要么全成要么全败**
		// （AGENTS「写操作」节），后端锁不到某一条就整批不删、明细放 data.items ——
		// 所以前端不做「按 items[].deleted 逐条移除行」，成功刷新、被拒由 toast 说原因。
		`hx-post="/api/product/brand/bulk-delete"`,
		`hx-post="/api/product/brand/delete"`,
		`data-reload-on-ok`,
		// 编辑抽屉与新建抽屉都是整表单档：两处字段清单相同，只差一个 id 隐藏域。
		// 这一档已从 ui/json_write.js 迁到 htmx（formdata）；判据不变 ——
		// 写入口必须声明端点，少了它按钮会退化成原生提交（页面 POST 的 action 早已删除）。
		`hx-post="/api/product/brand/update"`,
		`hx-post="/api/product/brand/create"`,
		`hx-swap="none"`,
		`data-close-on-ok="[data-drawer]"`,
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

// TestTagPageUsesJSONWriteBase 标签页的删除 / 批量删除 / 重算都走 JSON 写基座。
//
// Tags 里必须有一行**自动标签**（IsRule）：单条「重算」表单只在自动标签行上渲染，
// 只给手工标签时那条断言会因为表单根本不存在而假绿。
func TestTagPageUsesJSONWriteBase(t *testing.T) {
	h := taxonomyHandle(&productPageReaderStub{tagsView: productTagsPageViewStub(func(v *productdto.ProductTagsPageResp) {
		v.TagDetails = append(v.TagDetails, &productdto.TagResp{
			ID: "t2", Name: "自动标签", Slug: "auto",
			Kind: productenums.TagKindRule, RuleType: productenums.TagRuleNewArrival,
		})
		// 规则下拉必须带参数键清单：它是表单组装 ruleParams 的唯一依据（前端不另抄一份映射）。
		v.RuleTypes = []*productdto.TagRuleTypeResp{{
			Type: productenums.TagRuleNewArrival, ParamKeys: []string{"days"},
		}}
	})})
	rec := taxonomyJSONWriteRequest(t, h, "/admin/product-tags", "/admin/product-tags?project=p1", taxonomyAllButtons())
	if rec.Code != http.StatusOK {
		t.Fatalf("整页 GET 状态码 = %d, want 200", rec.Code)
	}
	html := rec.Body.String()
	assertContainsAll(t, html, []string{
		// 全部写入口都已迁到 htmx（formdata）。判据不变：每个动作都要声明端点，
		// 少了它按钮会退化成原生提交（页面 POST 的 action 早已删除）。
		"data-list-bulk",
		`hx-post="/api/product/tag/bulk-delete"`,
		`hx-post="/api/product/tag/delete"`,
		`data-reload-on-ok`,
		// 重算：全量按钮与行内按钮共用一个端点（tagId 空即全工程）
		`hx-post="/api/product/tag/recalc"`,
		`id="tag-recalc-all"`,
		`id="tag-recalc-t2"`,
		// 新建 / 编辑抽屉：规则参数的嵌套对象声明 —— 它现在是 htmx_form_body.js 读的契约
		`hx-post="/api/product/tag/create"`,
		`hx-post="/api/product/tag/update"`,
		`data-write-object=".tag-rule-fields;ruleParams;ruleType;kind=rule"`,
		`data-rule-keys="days"`,
		// 结果区（失败时才有内容）
		"data-write-status",
	})
	assertContainsNone(t, html, []string{
		`action="/admin/product-tags/bulk-delete"`,
		`action="/admin/product-tags/delete"`,
		`action="/admin/product-tags/recalc"`,
		// 标签 create/update 的页面 POST 本批删除：残留 = 按钮点了走整页跳转或者 404。
		`action="/admin/product-tags/create"`,
		`action="/admin/product-tags/update"`,
	})
}

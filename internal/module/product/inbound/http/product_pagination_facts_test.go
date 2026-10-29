package producthttp

// product_pagination_facts_test.go — P3 B2 的 handler 级判据（task-109）。
//
// 模板级差分（internal/templates）证明「同一份 partial 用两种数据形态渲染出同样的字节」，
// 这里补的是另一半：**handler 真的改成了事实键，且页面上出现的是可以独立复算的链接**。
//
// 期望值一律在测试里用 url.Values 独立拼（不复制产品代码的拼串逻辑）：链接里少一个筛选参数
// 或少了 limit，页面上看不出来（照样能翻页），只有独立复算能发现。
//
// 另有一档必须保住的**既有行为**（lead 裁定：本批不改）：商品列表页在 total>0 之后逐行取详情
// 失败时，失败页**仍然**带分页条 —— 旧 BuildPagination 拿到的就是这个 total。

import (
	"context"
	"errors"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
)

// paginationElement 拼出页面里应当出现的**整段元素**（分页链接的字节）。
//
// 为什么断言整段元素而不是 `href="…"`：只盯属性值会漏掉「元素类名/内层文字变了」这类消融
// （例如把 `<a class="pagination-btn">` 换成别的类、或把页码写死成 1），而那些同样是用户看得见的
// 变化。整段元素的字节相等把「链接地址 + 类名 + 页码文字」一次钉住。
func paginationElement(base string, page, limit int) string {
	return `<a class="pagination-btn" href="` + paginationHref(base, page, limit) + `">` + strconv.Itoa(page) + `</a>`
}

// paginationHref 把「基地址 + page/limit」拼成页面里应当出现的属性字节。
//
// 页面里的 href 是 Jet 转义过的（`&` → `&amp;`，与旧 {{l.URL}} 插值同形），因此期望值也走
// 同一条转义 —— 断言的是**实际字节**，不是浏览器解析后的值。
func paginationHref(base string, page, limit int) string {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	raw := base + sep + "page=" + strconv.Itoa(page) + "&limit=" + strconv.Itoa(limit)
	return strings.ReplaceAll(raw, "&", "&amp;")
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
					if strings.Contains(body, `class="pagination"`) {
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

// TestProductsPagePaginationFactsRenderLinks 商品列表页：三个筛选参数与 limit 都在链接里。
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

// —— B2b：命中商品片段的 handler 级判据（模板级差分在 internal/templates）——

// tagHitsStub 只实现片段用到的一个方法（其余因嵌入接口为 nil 而 panic）。
type tagHitsStub struct {
	productcontract.ProductService
	resp *productdto.TagProductsListResp
	err  error
}

func (s *tagHitsStub) ListTagProductsPage(context.Context, *productdto.ListTagProductsPageReq) (*productdto.TagProductsListResp, error) {
	return s.resp, s.err
}

// tagHitsRequest 把片段挂到最小引擎上，用真实 Jet 渲染器跑完整个请求。
func tagHitsRequest(t *testing.T, h *ProductPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/product-tags/hits", adminui.PageSlot(ProductTagHitsTemplate), h.ProductTagHitsFragment)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func tagHitsRows(n int) []*productdto.TagProductResp {
	rows := make([]*productdto.TagProductResp, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, &productdto.TagProductResp{ID: "p", Name: "商品", Slug: "sku", Status: "on"})
	}
	return rows
}

// TestTagHitsFragmentPaginationIsFactsDriven 片段走 handler 时：事实键必须注入、按钮是 hx-get、
// info 区间按**本页实际行数**算。
//
// 为什么这条必须存在：片段的模板级用例（internal/templates）直接喂数据，看不到 handler 是否
// 真的把 Pagination 交出去 —— 若 handler 按 HasPagination() 设闸，单页非空时模板读不到该键，
// 页面上就会变成「没有分页信息」，而模板级用例照样全绿。
func TestTagHitsFragmentPaginationIsFactsDriven(t *testing.T) {
	t.Run("第二页三行", func(t *testing.T) {
		// reader 不给：本片段（命中商品）不走读模型，读模型那一份由整页的用例覆盖。
		h := &ProductPageHandle{products: &tagHitsStub{resp: &productdto.TagProductsListResp{
			TagID: "t1", TagName: "标签", Total: 45, Page: 2, PageSize: 20,
			Items: tagHitsRows(3),
		}}}
		rec := tagHitsRequest(t, h, "/admin/product-tags/hits?project=p1&id=t1&page=2&size=20")
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码应为 200，实际 %d", rec.Code)
		}
		body := rec.Body.String()
		// 区间按实际行数（3 行）算 —— 不是按 total 推出的 21-40。
		if !strings.Contains(body, "共 45 条，第 21-23 条") {
			t.Fatalf("info 行没按实际行数算：%s", headOf(body))
		}
		// 整段按钮元素字节收口：地址（含 id/page/project 的键序）、类名、hx 三属性、按钮文字一次钉住。
		for _, want := range []string{
			tagHitsButtonElement(1, "上一页"),
			tagHitsButtonElement(3, "下一页"),
			`<div class="pagination-info">共 45 条，第 21-23 条</div>`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("片段缺少整段元素 %q", want)
			}
		}
		if strings.Contains(body, `<a class="pagination-btn"`) {
			t.Error("翻页入口出现了 <a href> 形态：本片段必须走 hx-get")
		}
		if strings.Contains(body, "limit=") {
			t.Error("片段翻页链接不该带 limit（KeepLimit=false）")
		}
	})

	t.Run("单页非空仍渲染两个禁用按钮", func(t *testing.T) {
		h := &ProductPageHandle{products: &tagHitsStub{resp: &productdto.TagProductsListResp{
			TagID: "t1", TagName: "标签", Total: 5, Page: 1, PageSize: 20,
			Items: tagHitsRows(5),
		}}}
		body := tagHitsRequest(t, h, "/admin/product-tags/hits?project=p1&id=t1&page=1&size=20").Body.String()
		if !strings.Contains(body, `class="pagination"`) {
			t.Fatal("单页非空时片段没渲染分页条（handler 若按 HasPagination() 设闸就会这样）")
		}
		for _, want := range []string{
			`<span class="pagination-btn is-disabled">上一页</span>`,
			`<span class="pagination-btn is-disabled">下一页</span>`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("单页档缺少整段禁用元素 %q", want)
			}
		}
		if got := strings.Count(body, `class="pagination-btn is-disabled"`); got != 2 {
			t.Fatalf("禁用按钮 = %d 个，期望 2 个（上一页 / 下一页）", got)
		}
		if !strings.Contains(body, "共 5 条，第 1-5 条") {
			t.Fatalf("info 行不对：%s", headOf(body))
		}
	})
}

// tagHitsButtonElement 命中商品片段里翻页按钮的整段字节（键序 id → page → project，属性按 HTML 转义）。
func tagHitsButtonElement(page int, label string) string {
	// 基址来自请求串（去掉 page/id/project/size 后只剩路径），参数按 id → page → project 重新拼 ——
	// 与迁移前逐字节一致，size 不跟翻页走（端点默认页容量）。
	return `<button type="button" class="pagination-btn" hx-get="/admin/product-tags/hits?id=t1&amp;page=` +
		strconv.Itoa(page) + `&amp;project=p1" hx-target="#tag-hits-panel" hx-swap="innerHTML">` + label + `</button>`
}

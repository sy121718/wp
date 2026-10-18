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
	projectdto "go_wp/internal/module/project/dto"
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
	const rows = 25 // 每页 20 → 两页，第 2 页一定在窗口里
	cases := []struct {
		name string
		path string
		base string
	}{
		{
			name: "标签页",
			path: "/admin/product-tags?project=p1&keyword=pg",
			base: "/admin/product-tags?keyword=pg&project=p1",
		},
		{
			name: "分类页",
			path: "/admin/product-categories?project=p1&keyword=pg",
			base: "/admin/product-categories?keyword=pg&project=p1",
		},
		{
			name: "品牌页",
			path: "/admin/product-brands?project=p1&keyword=pg",
			base: "/admin/product-brands?keyword=pg&project=p1",
		},
	}
	// 四页的取数编排都已下沉读模型：分页事实由 service 给、控制器只补基地址，
	// 桩因此一律给**视图**（不再 mock Count*/List* 那些方法 —— 它们已不在这条渲染路径上）。
	t.Run("属性页", func(t *testing.T) {
		h := &ProductPageHandle{
			projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
			products: &productPageReaderStub{attributesPageView: productAttributesPageViewStub(func(v *productdto.ProductAttributesPageResp) {
				v.SelectedProject = "p1"
				v.Total = rows
				v.FilterKeyword = "pg"
				v.FilterVariation = "1"
			})}, reader: &productPageReaderStub{attributesPageView: productAttributesPageViewStub(func(v *productdto.ProductAttributesPageResp) {
				v.SelectedProject = "p1"
				v.Total = rows
				v.FilterKeyword = "pg"
				v.FilterVariation = "1"
			})},
		}
		rec := productPageRequestFor(t, h, "/admin/product-attributes", "/admin/product-attributes?project=p1&keyword=pg&variation=1")
		if rec.Code != http.StatusOK {
			t.Fatalf("成功档应 200，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
		}
		body := rec.Body.String()
		for _, want := range []string{
			paginationElement("/admin/product-attributes?keyword=pg&project=p1&variation=1", 2, 20),
			`<span class="pagination-btn is-active">1</span>`,
			`<span class="pagination-btn is-disabled">上一页</span>`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("属性页分页条里没有整段元素 %q", want)
			}
		}
		for _, bad := range []string{"PaginationLinks", "PaginationInfo"} {
			if strings.Contains(body, bad) {
				t.Errorf("属性页仍出现旧键 %q", bad)
			}
		}
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &ProductPageHandle{
				projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
				products: &productPageReaderStub{
					categoriesView: productCategoriesPageViewStub(func(v *productdto.ProductCategoriesPageResp) {
						v.SelectedProject, v.Total, v.FilterKeyword = "p1", rows, "pg"
					}),
					brandsView: productBrandsPageViewStub(func(v *productdto.ProductBrandsPageResp) {
						v.SelectedProject, v.Total, v.FilterKeyword = "p1", rows, "pg"
					}),
					tagsView: productTagsPageViewStub(func(v *productdto.ProductTagsPageResp) {
						v.SelectedProject, v.Total, v.FilterKeyword = "p1", rows, "pg"
					}),
				}, reader: &productPageReaderStub{
					categoriesView: productCategoriesPageViewStub(func(v *productdto.ProductCategoriesPageResp) {
						v.SelectedProject, v.Total, v.FilterKeyword = "p1", rows, "pg"
					}),
					brandsView: productBrandsPageViewStub(func(v *productdto.ProductBrandsPageResp) {
						v.SelectedProject, v.Total, v.FilterKeyword = "p1", rows, "pg"
					}),
					tagsView: productTagsPageViewStub(func(v *productdto.ProductTagsPageResp) {
						v.SelectedProject, v.Total, v.FilterKeyword = "p1", rows, "pg"
					}),
				},
			}

			rec := productPageRequestFor(t, h, tc.path[:strings.Index(tc.path, "?")], tc.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("成功档应 200，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
			}
			body := rec.Body.String()
			if !strings.Contains(body, `class="pagination"`) {
				t.Fatalf("超过一页却没渲染分页条")
			}
			// 整段元素字节收口（不是只比 href 的值）：地址、类名、页码文字一次钉住。
			// 当前页（page=1）是 is-active 的 span、没有 href；能证明「基地址 + page + limit」
			// 全都在链接里的是第 2 页那条。
			for _, want := range []string{
				paginationElement(tc.base, 2, 20),
				`<span class="pagination-btn is-active">1</span>`,
				`<span class="pagination-btn is-disabled">上一页</span>`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("分页条里没有整段元素 %q", want)
				}
			}
			// 旧键必须已经不在页面上：留着它就是「两条链并存」，下一批迁移会把其中一条改漏。
			for _, bad := range []string{"PaginationLinks", "PaginationInfo"} {
				if strings.Contains(body, bad) {
					t.Errorf("页面上仍出现旧键 %q", bad)
				}
			}
		})
	}
}

// TestTaxonomyPagesSinglePageRendersNoPagination 单页/空集不渲染分页条（事实键不注入这一档）。
func TestTaxonomyPagesSinglePageRendersNoPagination(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "标签页", path: "/admin/product-tags?project=p1"},
		{name: "分类页", path: "/admin/product-categories?project=p1"},
		{name: "品牌页", path: "/admin/product-brands?project=p1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &ProductPageHandle{
				projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
				products: &productPageReaderStub{
					categoriesView: productCategoriesPageViewStub(func(v *productdto.ProductCategoriesPageResp) {
						v.SelectedProject, v.Total = "p1", 3
					}),
					brandsView: productBrandsPageViewStub(func(v *productdto.ProductBrandsPageResp) {
						v.SelectedProject, v.Total = "p1", 3
					}),
					tagsView: productTagsPageViewStub(func(v *productdto.ProductTagsPageResp) {
						v.SelectedProject, v.Total = "p1", 3
					}),
				}, reader: &productPageReaderStub{
					categoriesView: productCategoriesPageViewStub(func(v *productdto.ProductCategoriesPageResp) {
						v.SelectedProject, v.Total = "p1", 3
					}),
					brandsView: productBrandsPageViewStub(func(v *productdto.ProductBrandsPageResp) {
						v.SelectedProject, v.Total = "p1", 3
					}),
					tagsView: productTagsPageViewStub(func(v *productdto.ProductTagsPageResp) {
						v.SelectedProject, v.Total = "p1", 3
					}),
				},
			}
			body := productPageRequestFor(t, h, tc.path[:strings.Index(tc.path, "?")], tc.path).Body.String()
			if strings.Contains(body, `class="pagination"`) {
				t.Fatalf("单页不该渲染分页条")
			}
		})
	}

	// 属性页同一条判据（读模型给的总数只有 3 条 → Facts 的 HasPagination 为假 → 控制器不注入）。
	t.Run("属性页", func(t *testing.T) {
		h := &ProductPageHandle{
			projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
			products: &productPageReaderStub{attributesPageView: productAttributesPageViewStub(func(v *productdto.ProductAttributesPageResp) {
				v.SelectedProject = "p1"
				v.Total = 3
			})}, reader: &productPageReaderStub{attributesPageView: productAttributesPageViewStub(func(v *productdto.ProductAttributesPageResp) {
				v.SelectedProject = "p1"
				v.Total = 3
			})},
		}
		body := productPageRequestFor(t, h, "/admin/product-attributes", "/admin/product-attributes?project=p1").Body.String()
		if strings.Contains(body, `class="pagination"`) {
			t.Fatalf("单页不该渲染分页条")
		}
	})
}

// TestProductsPagePaginationFactsRenderLinks 商品列表页：三个筛选参数与 limit 都在链接里。
func TestProductsPagePaginationFactsRenderLinks(t *testing.T) {
	const rows = 25
	// 列表页的取数编排在 service（读模型）里，控制器只调它 —— 桩因此给**视图**，
	// 不再 mock List / CountProducts / Get（那些方法已不在这条渲染路径上）。
	h := &ProductPageHandle{
		projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
		products: &productPageReaderStub{listView: productListPageViewStub(func(v *productdto.ProductListPageResp) {
			v.Total = rows
			v.FilterKeyword = "abc"
			v.FilterStatus = "draft"
		})}, reader: &productPageReaderStub{listView: productListPageViewStub(func(v *productdto.ProductListPageResp) {
			v.Total = rows
			v.FilterKeyword = "abc"
			v.FilterStatus = "draft"
		})},
	}
	rec := productPageRequest(t, h, "/admin/products?project=p1&keyword=abc&status=draft")
	if rec.Code != http.StatusOK {
		t.Fatalf("成功档应 200，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
	}
	body := rec.Body.String()
	base := "/admin/products?keyword=abc&project=p1&status=draft"
	if !strings.Contains(body, paginationElement(base, 2, 20)) {
		t.Fatalf("商品列表页的分页链接元素不对（筛选 / limit / 类名 / 页码文字，期望整段 %q）",
			paginationElement(base, 2, 20))
	}
	if strings.Contains(body, "PaginationLinks") || strings.Contains(body, "PaginationInfo") {
		t.Fatal("页面上仍出现旧分页键")
	}
}

// TestProductsPageFailureAfterTotalIsJSON total 已确定后行详情失败：出口仍是 JSON。
//
// 失败页形态已废除（页面失败不渲染任何 HTML），分页事实（total）不跟着失败变成半张列表：
// 报文里既没有分页条，也没有 HTML 页壳。
func TestProductsPageFailureAfterTotalIsJSON(t *testing.T) {
	const rows = 25
	reader := &productPageReaderStub{
		listView: productListPageViewStub(func(v *productdto.ProductListPageResp) { v.Total = rows }),
		listErr:  errors.New("读取商品详情失败"),
	}
	h := &ProductPageHandle{
		projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
		products: reader,
		reader:   reader,
	}
	rec := productPageRequest(t, h, "/admin/products?project=p1")
	body := assertProductJSONFailure(t, "行详情失败", rec, http.StatusInternalServerError, "内部错误")
	if strings.Contains(body, `class="pagination"`) {
		t.Fatal("失败报文里出现了分页条")
	}
}

// —— B2b：命中商品片段的 handler 级判据（模板级差分在 internal/templates）——

// tagHitsStub 只实现片段用到的一个方法（其余因嵌入接口为 nil 而 panic）。
type tagHitsStub struct {
	productcontract.ProductService
	resp *productdto.TagProductsPageResp
	err  error
}

func (s *tagHitsStub) ListTagProductsPage(context.Context, *productdto.ListTagProductsPageReq) (*productdto.TagProductsPageResp, error) {
	return s.resp, s.err
}

// tagHitsRequest 把片段挂到最小引擎上，用真实 Jet 渲染器跑完整个请求。
func tagHitsRequest(t *testing.T, h *ProductPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/product-tags/hits", adminui.Page(ProductTagHitsTemplate, h.ProductTagHitsFragment))
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
		h := &ProductPageHandle{products: &tagHitsStub{resp: &productdto.TagProductsPageResp{
			TagID: "t1", TagName: "标签", Total: 45, Page: 2, PageSize: 20, TotalPage: 3,
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
		h := &ProductPageHandle{products: &tagHitsStub{resp: &productdto.TagProductsPageResp{
			TagID: "t1", TagName: "标签", Total: 5, Page: 1, PageSize: 20, TotalPage: 1,
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
	return `<button type="button" class="pagination-btn" hx-get="/admin/product-tags/hits?id=t1&amp;page=` +
		strconv.Itoa(page) + `&amp;project=p1" hx-target="#tag-hits-panel" hx-swap="innerHTML">` + label + `</button>`
}

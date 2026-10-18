package feature

// product_pagination_links_bytes_test.go — P3 B2 的端到端链接字节判据（task-109，纯新增）。
//
// 为什么还要一层 feature 级：模板级差分证明「同数据 → 同字节」，handler 级桩证明「注入了事实键」，
// 但只有真实 service + 真实迁移建出的表 + 真实 Jet 跑一遍，才能证明**基地址里带的筛选参数**
// 与 **limit** 在真实数据下确实出现在翻页链接里 —— 这一层此前只断言「分页条里有 page=2」
// 与「关键词没丢」（product_list_filter_paging_test.go），从没钉过 limit。
//
// 只增不改：既有断言保持原样，这里另起一个文件用真实数据把链接字节固定下来。

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

// linkWithPagination 独立复算「基地址 + page + limit」在 HTML 属性里的字节。
//
// 页面里的 href 是 Jet 转义过的（`&` → `&amp;`）；这里从 url.Values.Encode() 出发，
// 不复制产品代码的拼串逻辑 —— 少一个筛选参数或多一个参数都会在这里露出来。
func linkWithPagination(path string, filters url.Values, page, limit int) string {
	base := path
	if enc := filters.Encode(); enc != "" {
		base += "?" + enc
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	raw := fmt.Sprintf("%s%spage=%d&limit=%d", base, sep, page, limit)
	return strings.ReplaceAll(raw, "&", "&amp;")
}

// TestProductBrandsPaginationLinkBytes 品牌页：翻页链接保留关键词与工程，且带 limit=20。
func TestProductBrandsPaginationLinkBytes(t *testing.T) {
	engine, f := newTaxonomyPageEngine(t)
	if f == nil {
		return
	}
	const total = 25 // 每页 20 → 两页
	for i := 1; i <= total; i++ {
		if _, err := f.svc.CreateBrand(t.Context(), &productdto.CreateBrandReq{
			ProjectID: f.projectID,
			Name:      fmt.Sprintf("pg-links-%02d", i),
		}); err != nil {
			t.Fatalf("创建品牌失败: %v", err)
		}
	}
	body := getWholePage(t, engine, "/admin/product-brands?keyword=pg-links")
	nav := paginationSection(body)
	if nav == "" {
		t.Fatal("超过一页却没渲染分页条")
	}
	// 基地址来自请求串（除 page/limit 外原样回带）：本请求没带 project，链接里也就没有 ——
	// 不替用户补参数，缺省工程由服务端在取数时解析。
	want := paginationElement("/admin/product-brands", url.Values{
		"keyword": {"pg-links"},
	}, 2, 20)
	if !strings.Contains(nav, want) {
		t.Fatalf("翻页链接元素字节不对\n期望含: %s\n实际: %s", want, clip(nav, 600))
	}
}

// TestProductAttributesPaginationLinkBytes 属性页：多一维 variation，且带 limit=20。
func TestProductAttributesPaginationLinkBytes(t *testing.T) {
	engine, f := newAttrPageEngine(t)
	if f == nil {
		return
	}
	const total = 21 // 每页 20 → 两页，第 2 页 1 行
	for i := 1; i <= total; i++ {
		if _, err := f.svc.CreateAttribute(t.Context(), &productdto.CreateAttributeReq{
			ProjectID: f.projectID,
			Name:      fmt.Sprintf("pg-attr-link-%02d", i),
			Key:       fmt.Sprintf("pg-attr-link-%02d", i),
		}); err != nil {
			t.Fatalf("创建属性组失败: %v", err)
		}
	}
	body := getWholePage(t, engine, "/admin/product-attributes?project="+f.projectID+"&keyword=pg-attr-link")
	nav := paginationSection(body)
	if nav == "" {
		t.Fatal("超过一页却没渲染分页条")
	}
	want := paginationElement("/admin/product-attributes", url.Values{
		"keyword": {"pg-attr-link"},
		"project": {f.projectID},
	}, 2, 20)
	if !strings.Contains(nav, want) {
		t.Fatalf("翻页链接元素字节不对\n期望含: %s\n实际: %s", want, clip(nav, 600))
	}
}

// TestProductTaxonomySinglePageHasNoPaginationBar 单页数据不渲染分页条（事实键不注入这一档）。
func TestProductTaxonomySinglePageHasNoPaginationBar(t *testing.T) {
	engine, f := newTaxonomyPageEngine(t)
	if f == nil {
		return
	}
	if _, err := f.svc.CreateBrand(t.Context(), &productdto.CreateBrandReq{
		ProjectID: f.projectID,
		Name:      "pg-single-only",
	}); err != nil {
		t.Fatalf("创建品牌失败: %v", err)
	}
	body := getWholePage(t, engine, "/admin/product-brands?keyword=pg-single-only")
	if nav := paginationSection(body); nav != "" {
		t.Fatalf("单页不该渲染分页条：%s", clip(nav, 300))
	}
}

// paginationElement 页面里应当出现的**整段**分页链接元素（地址 + 类名 + 页码文字）。
//
// 断言整段元素而不是 `href="…"`：只比属性值会漏掉「类名变了」「页码文字写死」这类消融，
// 而那些同样是用户看得见的变化。
func paginationElement(path string, filters url.Values, page, limit int) string {
	return `<a class="pagination-btn" href="` + linkWithPagination(path, filters, page, limit) + `">` +
		strconv.Itoa(page) + `</a>`
}

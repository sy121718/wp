package feature

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

func linkWithPagination(path string, filters url.Values, page, size int) string {
	base := path
	if enc := filters.Encode(); enc != "" {
		base += "?" + enc
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	raw := fmt.Sprintf("%s%spage=%d&size=%d", base, sep, page, size)
	return strings.ReplaceAll(raw, "&", "&amp;")
}

func TestProductBrandsPaginationLinkBytes(t *testing.T) {
	engine, f := newTaxonomyPageEngine(t)
	if f == nil {
		return
	}
	const total = 25
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

	want := paginationElement("/admin/product-brands", url.Values{
		"keyword": {"pg-links"},
	}, 2, 20)
	if !strings.Contains(nav, want) {
		t.Fatalf("翻页链接元素字节不对\n期望含: %s\n实际: %s", want, clip(nav, 600))
	}
	second := getWholePage(t, engine, paginationTarget(t, nav, 2))
	if got := strings.Count(second, `name="ids"`); got != total-20 {
		t.Fatalf("跟随品牌第2页链接应有%d行，实际%d行", total-20, got)
	}

	custom := getWholePage(t, engine, "/admin/product-brands?keyword=pg-links&size=7")
	if got := strings.Count(custom, `name="ids"`); got != 7 {
		t.Fatalf("size=7应读取7行，实际%d行", got)
	}
	customTarget := paginationTarget(t, paginationSection(custom), 2)
	parsed, err := url.Parse(customTarget)
	if err != nil || parsed.Query().Get("size") != "7" || parsed.Query().Get("keyword") != "pg-links" {
		t.Fatalf("自定义分页丢失size或筛选条件：%q %v", customTarget, err)
	}
	customSecond := getWholePage(t, engine, customTarget)
	if got := strings.Count(customSecond, `name="ids"`); got != 7 || !strings.Contains(paginationSection(customSecond), "第 8-14 条") {
		t.Fatalf("跟随size=7链接应读取第8-14条，实际%d行：%s", got, clip(paginationSection(customSecond), 500))
	}
}

func TestProductAttributesPaginationLinkBytes(t *testing.T) {
	engine, f := newAttrPageEngine(t)
	if f == nil {
		return
	}
	const total = 21
	for i := 1; i <= total; i++ {
		if _, err := f.svc.CreateAttribute(t.Context(), &productdto.CreateAttributeReq{
			ProjectID:   f.projectID,
			Name:        fmt.Sprintf("pg-attr-link-%02d", i),
			Key:         fmt.Sprintf("pg-attr-link-%02d", i),
			IsVariation: boolPtr(true),
		}); err != nil {
			t.Fatalf("创建属性组失败: %v", err)
		}
	}
	if _, err := f.svc.CreateAttribute(t.Context(), &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "pg-attr-link-excluded", Key: "pg-attr-link-excluded", IsVariation: boolPtr(false),
	}); err != nil {
		t.Fatalf("创建反例属性组失败: %v", err)
	}
	body := getWholePage(t, engine, "/admin/product-attributes?project="+f.projectID+"&keyword=pg-attr-link&variation=1")
	nav := paginationSection(body)
	if nav == "" {
		t.Fatal("超过一页却没渲染分页条")
	}
	want := paginationElement("/admin/product-attributes", url.Values{
		"keyword":   {"pg-attr-link"},
		"project":   {f.projectID},
		"variation": {"1"},
	}, 2, 20)
	if !strings.Contains(nav, want) {
		t.Fatalf("翻页链接元素字节不对\n期望含: %s\n实际: %s", want, clip(nav, 600))
	}
	second := getWholePage(t, engine, paginationTarget(t, nav, 2))
	if got := strings.Count(second, `name="ids"`); got != 1 || strings.Contains(second, "pg-attr-link-excluded") {
		t.Fatalf("跟随属性第2页链接应只读取1个参与变体属性组，实际%d行：%s", got, clip(second, 600))
	}
}

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

func paginationElement(path string, filters url.Values, page, size int) string {
	return `<a class="pagination-btn" href="` + linkWithPagination(path, filters, page, size) + `">` +
		strconv.Itoa(page) + `</a>`
}

func paginationTarget(t *testing.T, nav string, page int) string {
	t.Helper()
	for _, match := range regexp.MustCompile(`href="([^"]+)"`).FindAllStringSubmatch(nav, -1) {
		target := html.UnescapeString(match[1])
		parsed, err := url.Parse(target)
		if err == nil && parsed.Query().Get("page") == strconv.Itoa(page) {
			return target
		}
	}
	t.Fatalf("分页条没有第%d页链接：%s", page, clip(nav, 600))
	return ""
}

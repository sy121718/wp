package templates

// admin_product_pagination_facts_test.go — P3 B2 的兼容安全网（task-109，设计 task-94 P3/B2）。
//
// 与 P0a/P2 同一手法：把 product 五个整页站点「旧 pkg/pagination 数据 + partial」的渲染字节
// 与「新事实 pagination.Page + partial」的渲染字节逐字节对照。Base/size 都取自 handler 的真实
// 调用点（见任务卡），所以这份对照同时钉住三件事：
//   · 基地址里的筛选参数逐条保留（keyword / project / status / variation）；
//   · `limit` 仍在链接里 —— 旧 BuildPagination 无条件追加，五个站点的链接字节里都有它
//     （products 页的 size 是常量 productListPageSize=20，并不从 query 取，省掉即字节变更）；
//   · 单页 / 空集两边都不渲染分页条（旧 nil → 空 map；新 HasPagination()==false → 不注入）。
//
// 另有 B2b 的 Jet 算术探针：命中商品片段的 info 行必须按**旧算法**用 len(Items) 计算
// （Sol 设计审 P2：product_tag.go 的 Count 与 List 是两次独立事务、无同一快照，并发下
// Items 可能少于期望行数，若改用 Facts 的 Start/End 句子会在那一刻变样），因此先证明
// Jet 能在模板里算这个表达式，再把它写进 product_tag_hits.html。

import (
	"strings"
	"testing"

	"go_wp/pkg/pagination"
)

// productPaginationSites product 五个整页站点的分页形态。
func productPaginationSites() []p0PaginationShape {
	return []p0PaginationShape{
		{
			// product_page.go:2899 商品列表：三个筛选（keyword/status 是产品特有维度）。
			name:  "B2-products-两页三参筛选",
			total: 45, page: 2, limit: 20,
			base: "/admin/products?keyword=abc&project=p1&status=draft",
		},
		{
			// 中段双侧省略号：窗口 7 的算法与旧实现相同（P2 的 oracle 已扫过全量）。
			name:  "B2-products-中段双省略号",
			total: 1000, page: 25, limit: 20,
			base: "/admin/products?project=p1",
		},
		{
			// 单页：两边都不渲染分页条。
			name:  "B2-products-单页不渲染",
			total: 5, page: 1, limit: 20,
			base: "/admin/products?keyword=abc&project=p1&status=draft",
		},
		{
			// 空集：旧 BuildPagination 返回 nil；新 Facts 的 Pages=0。
			name:  "B2-products-空集不渲染",
			total: 0, page: 1, limit: 20,
			base: "/admin/products?project=p1",
		},
		{
			// product_page.go:316 属性组：多一维 variation，且关键词是 CJK（编码同样是字节判据）。
			name:  "B2-attributes-三参含CJK",
			total: 45, page: 2, limit: 20,
			base: "/admin/product-attributes?keyword=%E9%A2%9C%E8%89%B2&project=p1&variation=1",
		},
		{
			// product_page.go:5659 标签页。
			name:  "B2-tags-两参",
			total: 25, page: 2, limit: 20,
			base: "/admin/product-tags?keyword=pg&project=p1",
		},
		{
			// product_page.go:6194 分类页：无筛选时基地址不带 `?`（分隔符判据）。
			name:  "B2-categories-无筛选参数",
			total: 25, page: 1, limit: 20,
			base: "/admin/product-categories",
		},
		{
			// product_page.go:6450 品牌页末页：next 禁用 + 首省略号。
			name:  "B2-brands-末页next禁用",
			total: 25, page: 2, limit: 20,
			base: "/admin/product-brands?keyword=pg-brand",
		},
	}
}

// TestProductPaginationFactsMatchLegacyBytes 五个站点的旧/新渲染必须逐字节相同。
func TestProductPaginationFactsMatchLegacyBytes(t *testing.T) {
	for _, s := range productPaginationSites() {
		t.Run(s.name, func(t *testing.T) {
			legacy := p2RenderLegacy(t, s)
			facts := p2RenderFacts(t, s, nil)
			if legacy != facts {
				t.Errorf("旧/新渲染不一致：%s", p2FirstDiff(legacy, facts))
			}
		})
	}
}

// TestProductPaginationSitesKeepDefaultParamAndLimit 五站点都用默认事实语义：页码参数 `page`
// 且保留 `limit` —— 链接少了 limit 是**静默**的字节变更（页面照样能翻，只有对照能发现）。
func TestProductPaginationSitesKeepDefaultParamAndLimit(t *testing.T) {
	for _, s := range productPaginationSites() {
		t.Run(s.name, func(t *testing.T) {
			pg := pagination.Facts(s.total, s.page, s.limit)
			if pg.PageParam != "page" {
				t.Errorf("PageParam = %q，期望默认 page", pg.PageParam)
			}
			if !pg.KeepLimit {
				t.Error("KeepLimit = false，产品的五个整页站点都要保留 limit")
			}
			if !pg.HasPagination() {
				// 单页/空集站点本来就不该有链接，跳过字节断言。
				return
			}
			out := p2RenderFacts(t, s, nil)
			if !strings.Contains(out, "limit=20") {
				t.Errorf("链接里丢了 limit=20：%s", p2FirstDiff("…limit=20…", out))
			}
		})
	}
}

// TestB2JetComputesInfoRangeFromRowCount B2b 的 info 行算法在 Jet 侧可算。
//
// 表达式与将写进 product_tag_hits.html 的逐字相同：from=(Page-1)*Limit+1、to=from+len(Items)-1。
// 探针用 3 行数据（而非常见的一整页 20 行）正是为了证明**算法取的是实际行数**：
// 若换成 Facts 的 Start/End，这里会得到 21-40 而不是 21-23。
func TestB2JetComputesInfoRangeFromRowCount(t *testing.T) {
	tpl := `{{pg := .Pagination}}{{from := (pg.Page - 1) * pg.Limit + 1}}{{to := from + len(.Items) - 1}}{{sprintf("%s|%s|%s", pg.Total, from, to)}}`
	rows := []any{"a", "b", "c"}
	out, err := p0plusRenderData(t, tpl, map[string]any{
		"Pagination": pagination.Facts(45, 2, 20),
		"Items":      rows,
	})
	if err != nil {
		t.Fatalf("渲染失败（Jet 不支持该算术形态时这里会报错）: %v", err)
	}
	if out != "45|21|23" {
		t.Fatalf("info 区间 = %q，期望 %q（必须按实际行数算，不是 Facts 的 Start/End）", out, "45|21|23")
	}
	// 反面判据：同一条模板换成 facts 的 End，会得出 40 —— 证明这条断言真的在钉算法。
	if got := pagination.Facts(45, 2, 20).End; got != 40 {
		t.Fatalf("Facts 的 End = %d，期望 40（本断言的对照值）", got)
	}
}

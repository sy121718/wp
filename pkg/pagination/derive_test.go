package pagination

// derive_test.go — 派生值的口径。
//
// 与 window_test.go 同一手法：把**迁移前的后台实现**逐字拷成 oracle，逐组合对照。
// 派生值是「显示形状」，错一位用户看到的就是「共 120 条，第 21-40 条」里的数字对不上 ——
// 那种错误不会报错、不会 500，只会让人算不清自己在看第几行。

import (
	"strings"
	"testing"
)

// legacyDerive 迁移前 internal/templates/pager.go 的 PagerOf 原文。
func legacyDerive(p Page) Derived {
	limit := p.Limit
	if limit < 1 {
		limit = DefaultPageSize
	}
	page := p.Page
	if page < 1 {
		page = 1
	}
	pages := 0
	if p.Total > 0 {
		pages = int((p.Total + int64(limit) - 1) / int64(limit))
	}
	if pages > 0 && page > pages {
		page = pages
	}
	d := Derived{Total: p.Total, Page: page, Limit: limit, Pages: pages,
		PageParam: p.PageParam, KeepLimit: !p.DropLimit, Base: p.Base}
	if d.PageParam == "" {
		d.PageParam = "page"
	}
	if p.Total > 0 {
		d.Start = (page-1)*limit + 1
		d.End = page * limit
		if int64(d.End) > p.Total {
			d.End = int(p.Total)
		}
	}
	return d
}

// TestDeriveMatchesLegacyEveryCombination 扫 total × page × limit 的组合逐项对照。
//
// 含负值与 0：这些是「URL 被手改」与「查询没返回总数」的真实输入，
// 旧实现靠夹紧处理，新代码必须同口径。
func TestDeriveMatchesLegacyEveryCombination(t *testing.T) {
	totals := []int64{-5, 0, 1, 19, 20, 21, 40, 100, 120, 999}
	pages := []int{-3, 0, 1, 2, 3, 5, 6, 7, 50, 999}
	limits := []int{-1, 0, 1, 5, 20, 50, 100}
	for _, total := range totals {
		for _, page := range pages {
			for _, limit := range limits {
				p := Page{Total: total, Page: page, Limit: limit, Base: "/admin/x"}
				got := Derive(p)
				want := legacyDerive(Page{Total: total, Page: page, Limit: limit, Base: "/admin/x"})
				if got != want {
					t.Fatalf("total=%d page=%d limit=%d：\n得到 %+v\n期望 %+v", total, page, limit, got, want)
				}
			}
		}
	}
}

// TestDeriveClampsAndDefaults 单项断言（读起来比对照表直观）：夹紧与默认值。
func TestDeriveClampsAndDefaults(t *testing.T) {
	d := Derive(Page{Total: 120, Page: 99, Limit: 20})
	if d.Page != 6 || d.Pages != 6 {
		t.Fatalf("页码超出上界应夹到末页：page=%d pages=%d", d.Page, d.Pages)
	}
	if d.Start != 101 || d.End != 120 {
		t.Fatalf("末页区间应为 101-120，实际 %d-%d", d.Start, d.End)
	}

	d = Derive(Page{Total: 120, Page: -3, Limit: 20})
	if d.Page != 1 || d.Start != 1 || d.End != 20 {
		t.Fatalf("页码下界应夹到首页：%+v", d)
	}

	d = Derive(Page{Total: 120, Page: 1, Limit: 0})
	if d.Limit != DefaultPageSize {
		t.Fatalf("limit 非法应回落 %d，实际 %d", DefaultPageSize, d.Limit)
	}
	if d.PageParam != "page" {
		t.Fatalf("页码参数名默认应为 page，实际 %q", d.PageParam)
	}

	empty := Derive(Page{})
	if empty.Pages != 0 || empty.Start != 0 || empty.End != 0 {
		t.Fatalf("空集不该有页数与区间：%+v", empty)
	}
}

// TestDerivedItemsSlotShape 槽位：省略号无链接、当前页无链接、其余有链接。
func TestDerivedItemsSlotShape(t *testing.T) {
	d := Derive(Page{Total: 120, Page: 3, Limit: 20, Base: "/admin/x"})
	items := d.Items()
	if len(items) == 0 {
		t.Fatalf("多于一页应有槽位")
	}
	for _, it := range items {
		switch {
		case it.Gap:
			if it.URL != "" {
				t.Fatalf("省略号不该有链接：%+v", it)
			}
		case it.Page == d.Page:
			if it.URL != "" {
				t.Fatalf("当前页不该有链接：%+v", it)
			}
		default:
			if it.URL == "" {
				t.Fatalf("其它页应有链接：%+v", it)
			}
		}
	}
}

// TestDerivedItemsURLOrder 链接里 page 在前、limit 在后（按调用顺序，不是字典序）。
//
// 这是既有语义：迁移前的 link() 也是无条件按序追加，改顺序会改页面字节。
func TestDerivedItemsURLOrder(t *testing.T) {
	d := Derive(Page{Total: 120, Page: 3, Limit: 20, Base: "/admin/x?keyword=a"})
	for _, it := range d.Items() {
		if it.Gap || it.Page == d.Page {
			continue
		}
		if !strings.Contains(it.URL, "keyword=a&page=") {
			t.Fatalf("筛选条件应在 page 之前，实际 %q", it.URL)
		}
		if !strings.Contains(it.URL, "&limit=20") {
			t.Fatalf("链接应带 limit，实际 %q", it.URL)
		}
		if strings.Contains(it.URL, "&amp;") {
			t.Fatalf("Go 侧不得预转义（渲染时才转一次），实际 %q", it.URL)
		}
	}
	// DropLimit：链接不带 limit。
	drop := Derive(Page{Total: 120, Page: 3, Limit: 20, Base: "/admin/x", DropLimit: true})
	for _, it := range drop.Items() {
		if it.Gap || it.Page == drop.Page {
			continue
		}
		if strings.Contains(it.URL, "limit=") {
			t.Fatalf("DropLimit 时链接不该带 limit，实际 %q", it.URL)
		}
	}
}

// TestDerivedPrevNextURL 上/下一页：可用时给链接，不可用时给空串。
func TestDerivedPrevNextURL(t *testing.T) {
	mid := Derive(Page{Total: 120, Page: 3, Limit: 20, Base: "/x"})
	if mid.PrevURL() == "" || mid.NextURL() == "" {
		t.Fatalf("中间页两个方向都应有链接")
	}
	if !strings.Contains(mid.PrevURL(), "page=2") || !strings.Contains(mid.NextURL(), "page=4") {
		t.Fatalf("链接目标不对：prev=%q next=%q", mid.PrevURL(), mid.NextURL())
	}

	first := Derive(Page{Total: 120, Page: 1, Limit: 20, Base: "/x"})
	if first.PrevURL() != "" {
		t.Fatalf("首页不该有上一页链接，实际 %q", first.PrevURL())
	}

	last := Derive(Page{Total: 120, Page: 6, Limit: 20, Base: "/x"})
	if last.NextURL() != "" {
		t.Fatalf("末页不该有下一页链接，实际 %q", last.NextURL())
	}
}

// TestDerivedWithBase 换基址后所有链接跟着换（渲染层从请求串推基址时用）。
func TestDerivedWithBase(t *testing.T) {
	d := Derive(Page{Total: 120, Page: 3, Limit: 20}).WithBase("/admin/y?k=1")
	for _, it := range d.Items() {
		if it.Gap || it.Page == d.Page {
			continue
		}
		if !strings.HasPrefix(it.URL, "/admin/y?k=1&") {
			t.Fatalf("槽位链接没跟着换基址：%q", it.URL)
		}
	}
	if !strings.HasPrefix(d.PrevURL(), "/admin/y?k=1&") {
		t.Fatalf("上一页链接没跟着换基址：%q", d.PrevURL())
	}
}

// TestDerivedZeroValueIsInert 零值不该有任何可渲染行为（模板据此不渲染分页条）。
func TestDerivedZeroValueIsInert(t *testing.T) {
	var zero Derived
	if zero.HasPagination() || zero.HasPrev() || zero.HasNext() {
		t.Fatalf("零值不该有可渲染行为：%+v", zero)
	}
	if len(zero.Items()) != 0 {
		t.Fatalf("零值不该有槽位")
	}
}

// TestDerivedIsUsableWithoutRequestContext 本包不依赖请求上下文 —— 这是它能在
// 可视化构建器（不经过 gin）复用的前提。
//
// 这条看似废话，但它挡住的是「顺手往这里加个 gin.Context 参数」：
// 一旦加了，internal/builder 就再也用不了它，而后台侧不会有任何测试变红。
func TestDerivedIsUsableWithoutRequestContext(t *testing.T) {
	// 纯函数式调用：只要三件事实就能算出完整渲染数据。
	items := Derive(Page{Total: 100, Page: 2, Limit: 20, Base: "/p"}).Items()
	if len(items) == 0 {
		t.Fatalf("纯事实输入应能算出槽位")
	}
}

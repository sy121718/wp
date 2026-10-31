package pagination

import (
	"strings"
	"testing"
)

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

func TestDerivedZeroValueIsInert(t *testing.T) {
	var zero Derived
	if zero.HasPagination() || zero.HasPrev() || zero.HasNext() {
		t.Fatalf("零值不该有可渲染行为：%+v", zero)
	}
	if len(zero.Items()) != 0 {
		t.Fatalf("零值不该有槽位")
	}
}

func TestDerivedIsUsableWithoutRequestContext(t *testing.T) {

	items := Derive(Page{Total: 100, Page: 2, Limit: 20, Base: "/p"}).Items()
	if len(items) == 0 {
		t.Fatalf("纯事实输入应能算出槽位")
	}
}

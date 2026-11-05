package templates

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/pagination"
)

var (
	gapSlot    = regexp.MustCompile(`<span class="pagination-btn is-disabled">…</span>`)
	activeSlot = regexp.MustCompile(`<span class="pagination-btn is-active">(\d+)</span>`)
	linkSlot   = regexp.MustCompile(`<a class="pagination-btn" href="([^"]*)">(\d+)</a>`)
)

type slot struct {
	Gap  bool
	Page int
	URL  string
}

func extractSlots(t *testing.T, html string) []slot {
	t.Helper()
	type hit struct {
		at int
		s  slot
	}
	var hits []hit
	for _, m := range gapSlot.FindAllStringIndex(html, -1) {
		hits = append(hits, hit{at: m[0], s: slot{Gap: true}})
	}
	for _, m := range activeSlot.FindAllStringSubmatchIndex(html, -1) {
		n, _ := strconv.Atoi(html[m[2]:m[3]])
		hits = append(hits, hit{at: m[0], s: slot{Page: n}})
	}
	for _, m := range linkSlot.FindAllStringSubmatchIndex(html, -1) {
		n, _ := strconv.Atoi(html[m[4]:m[5]])
		href := htmlEscapeUndo(html[m[2]:m[3]])
		hits = append(hits, hit{at: m[0], s: slot{Page: n, URL: href}})
	}

	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].at < hits[j-1].at; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	out := make([]slot, len(hits))
	for i, h := range hits {
		out[i] = h.s
	}
	return out
}

func htmlEscapeUndo(s string) string {
	return strings.ReplaceAll(s, "&amp;", "&")
}

func expectedSlots(t *testing.T, p pagination.Page) []slot {
	t.Helper()

	limit := p.Limit
	if limit < 1 {
		limit = pagination.DefaultPageSize
	}
	pages := 0
	if p.Total > 0 {
		pages = int((p.Total + int64(limit) - 1) / int64(limit))
	}
	cur := p.Page
	if cur < 1 {
		cur = 1
	}
	if pages > 0 && cur > pages {
		cur = pages
	}
	win := pagination.Window(cur, pages)
	param := p.PageParam
	if param == "" {
		param = "page"
	}
	out := make([]slot, 0, len(win))
	for _, it := range win {
		if it.Gap {
			out = append(out, slot{Gap: true})
			continue
		}
		if it.Page == cur {
			out = append(out, slot{Page: it.Page})
			continue
		}

		sep := "?"
		if strings.Contains(p.Base, "?") {
			sep = "&"
		}
		href := p.Base + sep + url.QueryEscape(param) + "=" + url.QueryEscape(strconv.Itoa(it.Page))
		if !p.DropLimit {
			href += "&limit=" + url.QueryEscape(strconv.Itoa(limit))
		}
		out = append(out, slot{Page: it.Page, URL: href})
	}
	return out
}

func TestPagerItemsSlotSequence(t *testing.T) {
	bases := []string{
		"/admin/product-brands",
		"/admin/product-brands?keyword=pg",
	}
	for _, dropLimit := range []bool{false, true} {
		for _, base := range bases {
			for pages := 2; pages <= 24; pages++ {
				for page := 1; page <= pages; page++ {
					p := pagination.Page{
						Total: int64(pages * 20), Page: page, Limit: 20,
						Base: base, DropLimit: dropLimit,
					}
					html := renderPagerPartial(t, p)
					got := extractSlots(t, html)
					want := expectedSlots(t, p)
					if len(got) != len(want) {
						t.Fatalf("dropLimit=%v base=%q pages=%d page=%d：槽位数 %d，期望 %d\n实际 %v\n期望 %v",
							dropLimit, base, pages, page, len(got), len(want), got, want)
					}
					for i := range want {
						if got[i] != want[i] {
							t.Fatalf("dropLimit=%v base=%q pages=%d page=%d：第 %d 个槽位 %+v，期望 %+v",
								dropLimit, base, pages, page, i, got[i], want[i])
						}
					}
				}
			}
		}
	}
}

func TestPagerItemsSinglePageRendersNothing(t *testing.T) {
	for _, p := range []pagination.Page{
		{Total: 0, Page: 1, Limit: 20, Base: "/x"},
		{Total: 20, Page: 1, Limit: 20, Base: "/x"},
		{Total: 5, Page: 1, Limit: 20, Base: "/x"},
	} {
		html := renderPagerPartial(t, p)
		if strings.Contains(html, "<nav") {
			t.Fatalf("total=%d 不该渲染分页条，实际 %q", p.Total, html)
		}
	}
}

func TestPagerItemsPrevNextURLs(t *testing.T) {
	p := pagination.Page{Total: 120, Page: 3, Limit: 20, Base: "/admin/x"}
	html := renderPagerPartial(t, p)
	for _, want := range []string{
		`href="/admin/x?page=2&amp;limit=20">上一页</a>`,
		`href="/admin/x?page=4&amp;limit=20">下一页</a>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("缺少 %q\n实际: %s", want, html)
		}
	}

	first := renderPagerPartial(t, pagination.Page{Total: 120, Page: 1, Limit: 20, Base: "/admin/x"})
	if strings.Contains(first, "上一页</a>") {
		t.Fatalf("首页的上一页不该是链接：%s", first)
	}
	if !strings.Contains(first, `<span class="pagination-btn is-disabled">上一页</span>`) {
		t.Fatalf("首页的上一页应是禁用态：%s", first)
	}
}

func TestPagerItemsHasNoArithmeticInTemplate(t *testing.T) {

	src := stripJetComments(templateSource(t, "admin/partials/pagination.html"))

	for _, banned := range []string{"pageNumbers", "n-prev", "prev = n", "queryURL"} {
		if strings.Contains(src, banned) {
			t.Fatalf("partial 里仍出现 %q：窗口与取址应在 pkg/pagination，模板只判断三个分支", banned)
		}
	}
	for _, want := range []string{"pg.Items()", "it.Gap", "pg.PrevURL()", "pg.NextURL()"} {
		if !strings.Contains(src, want) {
			t.Fatalf("partial 缺少 %q：模板应直接消费后端算好的槽位", want)
		}
	}
}

func renderPagerPartial(t *testing.T, p pagination.Page) string {
	t.Helper()
	set := jet.NewSet(newAdminLoader(t), jet.WithTemplateNameExtensions([]string{"", ".html"}))
	injectGlobals(set)
	tpl, err := set.GetTemplate("admin/partials/pagination.html")
	if err != nil {
		t.Fatalf("编译 partial 失败: %v", err)
	}
	var buf strings.Builder
	data := map[string]any{
		"Pagination": p,

		"t": func(_ string, fallback string) string { return fallback },
	}
	if err := tpl.Execute(&buf, nil, data); err != nil {
		t.Fatalf("渲染 partial 失败: %v", err)
	}
	return buf.String()
}

func templateSource(t *testing.T, name string) string {
	t.Helper()
	return layeredTemplateSource(t, name)
}

func newAdminLoader(t *testing.T) jet.Loader {
	t.Helper()
	loader, err := newAdminTemplateLoader()
	if err != nil {
		t.Fatalf("构建 embed loader 失败: %v", err)
	}
	return loader
}

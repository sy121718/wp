package templates

// pager_items_render_test.go — 分页条「后端算好、模板无脑渲染」之后的判据。
//
// 改造前：模板自己 range 整数页码、用 {{if n-prev > 1}} 画省略号、用 queryURL 拼每个 href。
// 改造后：pager(.Pagination).Items() 直接给出槽位（含省略号位与已拼好的 URL），
// 模板只剩三个判据（it.Gap / it.Page == pg.Page / 其它）。
//
// —— 为什么不用「旧模板逐字节」做 oracle ——
//
// 试过，放弃了：旧模板的空白形态是「range 体内缩进 + 槽注释行 + 分支换行」共同决定的，
// 手工复刻它等于把模板的缩进规则抄一遍。那种测试的价值是「改了缩进会红」——
// 而缩进恰恰是这次改造要动的东西，抄一遍只会在下次改模板时制造假红。
//
// 换成三条更硬的判据：
//  1. **元素序列**：从真实渲染出的 HTML 里抽出「省略号 / 当前页 / 链接(页码,href)」序列，
//     与 pkg/pagination 的数据层期望逐个对齐。这才是有语义的等价。
//  2. **逐页穷举**：pages×page 全组合都抽一遍，包括贴边平移与越界夹紧。
//  3. **模板无算术**：源码里不许再出现 pageNumbers / n-prev / queryURL。

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/pagination"
)

// slot / activeSlot / linkSlot 从渲染结果里识别三种槽位。
var (
	gapSlot    = regexp.MustCompile(`<span class="pagination-btn is-disabled">…</span>`)
	activeSlot = regexp.MustCompile(`<span class="pagination-btn is-active">(\d+)</span>`)
	linkSlot   = regexp.MustCompile(`<a class="pagination-btn" href="([^"]*)">(\d+)</a>`)
)

// slot 是一个被识别出来的槽位（与 pagination.Item 同构，便于对照）。
type slot struct {
	Gap  bool
	Page int
	URL  string
}

// extractSlots 按出现顺序抽出页码区里的槽位。
//
// 只取 <nav class="pagination"> 之后的内容中的**页码链接**与**省略号**；
// 上/下一页按钮的文字是「上一页」「下一页」而不是数字，正则天然排除，
// 所以不需要额外切分区域。
func extractSlots(t *testing.T, html string) []slot {
	t.Helper()
	type hit struct {
		at   int
		s    slot
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
	// 按在 HTML 里出现的先后排序：这正是用户在页面上看到的顺序。
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

// htmlEscapeUndo 把 Jet 插值产生的转义还原成 URL 原值（只处理 &amp;，分页 URL 只会出现它）。
func htmlEscapeUndo(s string) string {
	return strings.ReplaceAll(s, "&amp;", "&")
}

// expectedSlots 数据层期望：窗口槽位 + 每页应得的 href（当前页无链接）。
//
// 独立于 partial 复算一遍 href —— 期望值不复用 Pager.pageURL，否则「两边一起错」也会绿。
func expectedSlots(t *testing.T, p pagination.Page) []slot {
	t.Helper()
	// 页数与夹紧口径与 PagerOf 一致（这里独立算一遍，不复用 Pager 的派生逻辑）。
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
			out = append(out, slot{Page: it.Page}) // 当前页：无链接
			continue
		}
		// 按**调用顺序**追加（page 在前、limit 在后），不是 url.Values.Encode 的字典序：
		// 生产路径是 pagination.QueryURL(base, param, n, "limit", limit)，
		// 顺序是既有语义（迁移前的 link() 也是无条件按序追加）。
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

// TestPagerItemsSlotSequence 逐页穷举：渲染出的槽位序列必须与数据层期望逐项相等
// （省略号位置、当前页位置、每个 href）。
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

// TestPagerItemsSinglePageRendersNothing 单页与空集不渲染分页条（判据是「有没有 nav」）。
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

// TestPagerItemsPrevNextURLs 上/下一页按钮的 href 指向相邻页，不可用时是禁用态。
//
// 这两种按钮的文字是「上一页」「下一页」，不被 extractSlots 当成页码槽位，
// 所以单独断言它们的 href。
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
	// 首页：上一页是禁用 span，不是链接。
	first := renderPagerPartial(t, pagination.Page{Total: 120, Page: 1, Limit: 20, Base: "/admin/x"})
	if strings.Contains(first, "上一页</a>") {
		t.Fatalf("首页的上一页不该是链接：%s", first)
	}
	if !strings.Contains(first, `<span class="pagination-btn is-disabled">上一页</span>`) {
		t.Fatalf("首页的上一页应是禁用态：%s", first)
	}
}

// TestPagerItemsHasNoArithmeticInTemplate 模板里不许再出现窗口算术与取址。
//
// 这条挡的是「有人图快，又在模板里写 {{if n-prev > 1}}」——那会让省略号重新变成
// 模板的知识，也就重新引入「改样式得先读懂算法」。
func TestPagerItemsHasNoArithmeticInTemplate(t *testing.T) {
	// 只查**代码部分**：Jet 注释 {* … *} 里提到旧写法（说明为什么改）是允许的，
	// 那种提及正是给下一个人看的上下文。
	src := stripJetComments(templateSource(t, "admin/partials/pagination.html"))
	// pageBase 不在禁令里：它是「Base 为空时从 req_path 现推基址」的兜底，
	// 处理的是**请求事实**（当前 URL），不是窗口算术。放宽它是有意的。
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

// renderPagerPartial 用**生产同配置**（embed loader + injectGlobals）真实渲染 partial。
//
// 走 embed 而不是读磁盘：partial 与页面一样可能在打包时被漏掉，而漏掉的表现是
// 「生产模式页面少一块」。顺带保证测试跑的就是要发布的那份字节。
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
		// t 是渲染出口注入的取词函数：本测试用中文兜底（各按钮的中文），
		// 与 expectedSlots 里不涉及的文案解耦。
		"t": func(_ string, fallback string) string { return fallback },
	}
	if err := tpl.Execute(&buf, nil, data); err != nil {
		t.Fatalf("渲染 partial 失败: %v", err)
	}
	return buf.String()
}

// templateSource 读模板源码（embed，与生产同一份）。
func templateSource(t *testing.T, name string) string {
	t.Helper()
	b, err := adminTemplatesFS.ReadFile(name)
	if err != nil {
		t.Fatalf("读模板 %s 失败: %v", name, err)
	}
	return string(b)
}

// newAdminLoader 生产同源的 embed loader（失败即测试缺陷，直接 Fatal）。
func newAdminLoader(t *testing.T) jet.Loader {
	t.Helper()
	loader, err := newAdminTemplateLoader()
	if err != nil {
		t.Fatalf("构建 embed loader 失败: %v", err)
	}
	return loader
}

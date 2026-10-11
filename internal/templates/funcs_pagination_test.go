package templates

// funcs_pagination_test.go — 分页展示层三个全局函数（sprintf / queryURL / pageNumbers）的测试
// （task-94 P1 / task-99 落地）。
//
// 这一层是 P1 的「新增展示工具」，所以判据有两面：
//   - **Go 侧语义**：位置填充、URL 编码、窗口算法必须逐项钉死（尤其编码要与 url.Values 同口径、
//     窗口要与迁移前的旧算法逐页相同）。
//   - **Jet 侧可用性**：这些函数真的能在属性位/文本位/`{{range}}` 里被调用，且 HTML 转义
//     恰好发生一次（Go 侧预转义会让链接静默变成 `&amp;amp;`）。
//
// 内联模板统一走 admin_pagination_globals_p0plus_test.go 的 p0plusRenderData：它用生产同配置的
// Set + injectGlobals，且**只在生产全局缺失时才注册替身**（不影子化）—— 也就是说 P1 落地后
// 那一组探针自动改测真实实现。为了让「全局忘了注册」不会退化成"测替身照样绿"，本文件另有
// TestPaginationGlobalsAreRegisteredByInjectGlobals 直接查注册表。

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// TestPaginationGlobalsAreRegisteredByInjectGlobals 三个函数必须由 injectGlobals 注册。
//
// 这条看着多余，其实是防"影子化"的唯一保险：P0b+ 的替身注册机制会在生产全局缺失时补上替身，
// 于是「没注册」这件事在模板探针里是**看不出来**的（照样渲染成功）。这里直接查注册表，
// 缺一个就红。
func TestPaginationGlobalsAreRegisteredByInjectGlobals(t *testing.T) {
	set := memSet(t, map[string]string{"probe/probe.html": "x"})
	injectGlobals(set)
	for _, name := range []string{"sprintf", "queryURL", "pageNumbers"} {
		if _, ok := set.LookupGlobal(name); !ok {
			t.Fatalf("全局 %q 未被 injectGlobals 注册 —— 模板里会渲染成函数地址而不是调用结果", name)
		}
	}
}

// TestQueryURLEscapesValuesLikeURLValues 值的编码必须与 url.Values.Encode 同口径
// （空格 → `+`、`&`/`=`/`#`/`%` 逐字符转义、非 ASCII 走 UTF-8 百分号编码）。
// 用 url.Values 现算期望值而不是手写字符串：口径漂移时这条会红，而手写期望只会一起漂。
func TestQueryURLEscapesValuesLikeURLValues(t *testing.T) {
	for _, v := range []any{"&", "#", "%", "=", "+", "?", "a b", "汉 字", "~/!*'()", "", "page=1&x=2"} {
		got := queryURL("/admin/x", "q", v)
		want := "/admin/x?" + url.Values{"q": {stringify(v)}}.Encode()
		if got != want {
			t.Fatalf("值 %q：queryURL = %q，url.Values 口径 = %q", v, got, want)
		}
		if strings.Contains(got, "&amp;") {
			t.Fatalf("值 %q：Go 侧不得预转义（Jet 渲染时才转义一次），实际 %q", v, got)
		}
	}
}

// TestQueryURLSeparatorOrderAndDuplicates 分隔符、顺序、重复键、以及"孤键忽略"的既有语义。
func TestQueryURLSeparatorOrderAndDuplicates(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"空 base 回空串", queryURL("", "page", 1), ""},
		{"全空白 base 回空串", queryURL("   ", "page", 1), ""},
		{"无查询串用 ?", queryURL("/admin/x", "page", 2), "/admin/x?page=2"},
		{"已有查询串用 &", queryURL("/admin/x?status=1", "page", 2), "/admin/x?status=1&page=2"},
		{"现行链接形态逐字不变", queryURL("/admin/x", "page", 2, "limit", 20), "/admin/x?page=2&limit=20"},
		{"顺序按调用顺序、不排序", queryURL("/admin/x", "b", 1, "a", 2), "/admin/x?b=1&a=2"},
		{"同名键不去重", queryURL("/admin/x", "b", 1, "b", 2), "/admin/x?b=1&b=2"},
		{"base 已含同名参数时原样追加", queryURL("/admin/x?page=1", "page", 2), "/admin/x?page=1&page=2"},
		{"孤键忽略", queryURL("/admin/x", "page"), "/admin/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("得到 %q，期望 %q", tc.got, tc.want)
			}
		})
	}
}

// TestQueryURLPanicsOnNonStringKey 键写错必须当场爆掉：静默丢一个筛选条件的链接，
// 表现是"用户点下一页后筛选没了"，比 500 难发现得多（与 fill 同一取舍）。
func TestQueryURLPanicsOnNonStringKey(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("非字符串键必须 panic")
		}
		if !strings.Contains(fmt.Sprint(r), "queryURL") {
			t.Fatalf("panic 里应指明函数名，实际: %v", r)
		}
	}()
	_ = queryURL("/admin/x", 1, "v")
}

// TestQueryURLRendersEscapedExactlyOnce 属性位与文本位都必须**恰好转义一次**。
//
// 这是分页链接最容易静默坏掉的地方：Go 侧若预先 html.EscapeString/QueryEscape 整个串，
// 页面就会渲染出 `&amp;amp;`（浏览器把它当成字面量 `&amp;` 参数）—— 不报错、不 500，只是筛选失效。
func TestQueryURLRendersEscapedExactlyOnce(t *testing.T) {
	out, err := p0plusRenderData(t,
		`<a href="{{queryURL("/admin/mail", "page", 2, "limit", 20)}}">x</a>|{{queryURL("/admin/mail", "page", 2, "limit", 20)}}`, nil)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if strings.Contains(out, "&amp;amp;") {
		t.Fatalf("出现双重转义（Go 侧预转义 + Jet 再转义）: %q", out)
	}
	if !strings.Contains(out, `href="/admin/mail?page=2&amp;limit=20"`) {
		t.Fatalf("属性位应恰好转义一次，实际: %q", out)
	}
	if !strings.Contains(out, "|/admin/mail?page=2&amp;limit=20") {
		t.Fatalf("文本位也应恰好转义一次，实际: %q", out)
	}
}

// TestSprintfFillsPositionalPlaceholders 位置填充：入参形态既可能是模板字面量（float64），
// 也可能是结构体字段（真 int64/int），两者都要填对。
func TestSprintfFillsPositionalPlaceholders(t *testing.T) {
	if got, want := sprintf("共 %s 条，第 %s-%s 条", int64(120), 21, 40), "共 120 条，第 21-40 条"; got != want {
		t.Fatalf("Go 侧填充得到 %q，期望 %q", got, want)
	}
	// 结构体字段形态（真实 partial 的用法）。取词函数先绑成变量再进参数位：
	// `.["t"](...)` 直接写在函数参数里 Jet 不认（unexpected node），绑一次即可。
	out, err := p0plusRenderData(t,
		`{{t := .["t"]}}{{pg := .["Pagination"]}}{{sprintf(t("shell.pagination.info", "共 %s 条，第 %s-%s 条"), pg.Total, pg.Start, pg.End)}}`,
		map[string]any{
			"t":          TranslateFunc("zh-CN"),
			"Pagination": probePage{Total: 120, Page: 2, Limit: 20, Pages: 6, Start: 21, End: 40},
		})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != "共 120 条，第 21-40 条" {
		t.Fatalf("真实形态填充得到 %q", out)
	}
	// 模板字面量形态（float64）——不先 stringify 就会渲染成 %!s(float64=120)
	out, err = p0plusRenderData(t, `{{sprintf("共 %s 条", 120)}}`, nil)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != "共 120 条" || strings.Contains(out, "%!") {
		t.Fatalf("模板字面量应填成「共 120 条」，实际 %q", out)
	}
}

// TestSprintfHazardPredicate 词条被改坏/参数个数写错时必须留下 `%!` 痕迹——这是 Warn 判据的基础，
// 也让"悄悄渲染一句少了个数字的话"这种情况在测试里可见。
func TestSprintfHazardPredicate(t *testing.T) {
	if got := sprintf("共 %s 条，第 %s-%s 条", 1); !strings.Contains(got, "%!") {
		t.Fatalf("参数不足应留下 %%! 痕迹，实际 %q", got)
	}
	if got := sprintf("共 %s 条", 1, 2); !strings.Contains(got, "%!") {
		t.Fatalf("参数多余应留下 %%! 痕迹，实际 %q", got)
	}
	if got := sprintf(""); got != "" {
		t.Fatalf("空 pattern 应回空串，实际 %q", got)
	}
	if got := sprintf("共 %s 条", nil); got != "共  条" {
		t.Fatalf("nil 值应走 stringify 成空串，实际 %q", got)
	}
}

// legacyNumberLabels 把迁移前 BuildPagination 的数字/省略号分支**逐字拷进来**作 oracle。
//
// 为什么不直接调用旧实现：旧实现把标签与链接一起产出（`PageLink{Label, URL}`），
// 中间还夹着 prev/next；而 pageNumbers 只负责"窗口里有哪些数字"。把旧算法的数字部分
// 原样复制成 oracle，才能逐页比较"窗口是否真的没变"——这是 P1 唯一能自证"窗口策略未漂移"的办法。
func legacyNumberLabels(page, pages int) []string {
	win := 7 // 迁移前的 MaxPageLinks
	var out []string
	if pages <= win {
		for p := 1; p <= pages; p++ {
			out = append(out, strconv.Itoa(p))
		}
		return out
	}
	half := (win - 3) / 2
	lo := page - half
	hi := page + half
	if lo < 2 {
		lo, hi = 2, lo+win-3
	}
	if hi > pages-1 {
		hi, lo = pages-1, pages-1-(win-3)
	}
	out = append(out, "1")
	if lo > 2 {
		out = append(out, "…")
	}
	for p := lo; p <= hi; p++ {
		if p < 2 || p > pages-1 {
			continue
		}
		out = append(out, strconv.Itoa(p))
	}
	if hi < pages-1 {
		out = append(out, "…")
	}
	return append(out, strconv.Itoa(pages))
}

// renderedNumberLabels 把 pageNumbers 的输出按"模板怎么画"还原成同一形态的标签序列：
// 相邻数字差 > 1 就在中间画一个省略号（省略号位置由模板按事实算，不在 Go 里）。
func renderedNumberLabels(page, pages int) []string {
	var out []string
	prev := 0
	for _, n := range pageNumbers(page, pages, 7) {
		if prev > 0 && n-prev > 1 {
			out = append(out, "…")
		}
		out = append(out, strconv.Itoa(n))
		prev = n
	}
	return out
}

// TestPageNumbersMatchesLegacyWindow 扫 pages=2..20 × page=1..pages，逐页对照旧算法。
//
// 从 2 起步而不是 1：旧实现在单页时**根本不产出页码**（它连分页条都不渲染），
// 而 pageNumbers 的口径是「pages<=1 → 空窗口」——两者的差别由 TestPageNumbersEdges 单独钉。
func TestPageNumbersMatchesLegacyWindow(t *testing.T) {
	for pages := 2; pages <= 20; pages++ {
		for page := 1; page <= pages; page++ {
			got := renderedNumberLabels(page, pages)
			want := legacyNumberLabels(page, pages)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("pages=%d page=%d：窗口 %v，旧实现 %v", pages, page, got, want)
			}
		}
	}
}

// TestPageNumbersEdges 边界：无分页 → 空窗口；页码越界先夹紧（与事实层同口径）；
// 很小/很大的 win 都要给出确定性结果（不死循环、不产出负数长度）。
func TestPageNumbersEdges(t *testing.T) {
	for _, pages := range []int{0, 1, -3} {
		if got := pageNumbers(1, pages, 7); len(got) != 0 {
			t.Fatalf("pages=%d 应回空窗口，实际 %v", pages, got)
		}
	}
	if got := pageNumbers(99, 6, 7); strings.Join(itoaAll(got), ",") != strings.Join(legacyNumberLabels(6, 6), ",") {
		t.Fatalf("页码越界应先夹到末页，实际 %v", got)
	}
	if got := pageNumbers(0, 5, 7); strings.Join(itoaAll(got), ",") != strings.Join(legacyNumberLabels(1, 5), ",") {
		t.Fatalf("页码下界应先夹到首页，实际 %v", got)
	}
	if got := pageNumbers(3, 100, 1); len(got) == 0 {
		t.Fatalf("win 过窄应退化成确定性的最小形态，实际 %v", got)
	}
	if got := pageNumbers(1, 1, 0); len(got) != 0 {
		t.Fatalf("单页应回空窗口，实际 %v", got)
	}
}

// TestPageNumbersRendersInTemplate 真实 Jet：range 覆 []int、空窗口走 else、
// 省略号按相邻差值在模板里画出。
func TestPageNumbersRendersInTemplate(t *testing.T) {
	out, err := p0plusRenderData(t, `{{range _, n := pageNumbers(2, 3, 7)}}[{{n}}]{{end}}`, nil)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != "[1][2][3]" {
		t.Fatalf("窗口渲染得到 %q", out)
	}

	out, err = p0plusRenderData(t, `{{if len(pageNumbers(1, 1, 7)) > 0}}X{{else}}EMPTY{{end}}`, nil)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != "EMPTY" {
		t.Fatalf("空窗口必须走 else，实际 %q", out)
	}

	out, err = p0plusRenderData(t,
		`{{prev := 0}}{{range _, n := pageNumbers(5, 20, 7)}}{{if prev > 0}}{{if n - prev > 1}}…{{end}}{{end}}[{{n}}]{{prev = n}}{{end}}`, nil)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	want := "[1]…[3][4][5][6][7]…[20]"
	if out != want {
		t.Fatalf("窗口与省略号渲染得到 %q，期望 %q", out, want)
	}
}

// probePage 是本文件的渲染数据形态：字段名与 pkg/pagination.Page 一致，
// 但这里刻意不复用它——P1 不改 partial，模板侧还没绑定到真实类型；
// 真实类型与模板惯用法的绑定由 P0b/P0b+ 的探针与后续 P2 承担。
type probePage struct {
	Total     int64
	Page      int
	Limit     int
	Pages     int
	Start     int
	End       int
	PageParam string
	KeepLimit bool
	Base      string
}

func itoaAll(in []int) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = strconv.Itoa(v)
	}
	return out
}

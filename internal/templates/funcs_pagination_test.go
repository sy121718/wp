package templates

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestPaginationGlobalsAreRegisteredByInjectGlobals(t *testing.T) {
	set := memSet(t, map[string]string{"probe/probe.html": "x"})
	injectGlobals(set)
	for _, name := range []string{"sprintf", "queryURL", "pageNumbers"} {
		if _, ok := set.LookupGlobal(name); !ok {
			t.Fatalf("全局 %q 未被 injectGlobals 注册 —— 模板里会渲染成函数地址而不是调用结果", name)
		}
	}
}

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

func TestQueryURLPanicsOnNonStringKey(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("非字符串键必须 panic")
		}
		if !strings.Contains(strings.ToLower(fmt.Sprint(r)), "queryurl") {
			t.Fatalf("panic 里应指明函数名，实际: %v", r)
		}
	}()
	_ = queryURL("/admin/x", 1, "v")
}

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

func TestSprintfFillsPositionalPlaceholders(t *testing.T) {
	if got, want := sprintf("共 %s 条，第 %s-%s 条", int64(120), 21, 40), "共 120 条，第 21-40 条"; got != want {
		t.Fatalf("Go 侧填充得到 %q，期望 %q", got, want)
	}

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

	out, err = p0plusRenderData(t, `{{sprintf("共 %s 条", 120)}}`, nil)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != "共 120 条" || strings.Contains(out, "%!") {
		t.Fatalf("模板字面量应填成「共 120 条」，实际 %q", out)
	}
}

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

func legacyNumberLabels(page, pages int) []string {
	win := 7
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

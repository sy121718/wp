package templates

import (
	"fmt"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/pagination"
)

func p0plusStandInPageNumbers(page, pages, win int) []int {
	if pages <= 1 {
		return nil
	}
	out := []int{1}
	for i := 2; i <= pages; i++ {
		out = append(out, i)
	}
	return out
}

func p0plusStandInSprintf(pattern string, args ...any) string {
	vals := make([]any, len(args))
	for i, a := range args {
		vals[i] = stringify(a)
	}
	return fmt.Sprintf(pattern, vals...)
}

func p0plusStandInQueryURL(base string, kv ...any) string {
	if strings.TrimSpace(base) == "" {
		return ""
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	vals := make([]string, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			panic(fmt.Sprintf("queryURL: 键必须是字符串，得到 %T", kv[i]))
		}
		vals = append(vals, key+"="+strings.ReplaceAll(stringify(kv[i+1]), " ", "+"))
	}
	if len(vals) == 0 {
		return base
	}
	return base + sep + strings.Join(vals, "&")
}

func p0plusGlobal(t *testing.T, set *jet.Set, name string, impl any) {
	t.Helper()
	if _, found := set.LookupGlobal(name); found {
		t.Logf("全局 %q 已由生产注册，探针直接测真实实现（不覆盖）", name)
		return
	}
	set.AddGlobal(name, impl)
}

func p0plusRenderData(t *testing.T, tpl string, data map[string]any) (out string, err error) {
	t.Helper()
	l := jet.NewInMemLoader()
	l.Set("probe/globals.html", tpl)
	set := jet.NewSet(l, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	injectGlobals(set)
	p0plusGlobal(t, set, "pageNumbers", p0plusStandInPageNumbers)
	p0plusGlobal(t, set, "sprintf", p0plusStandInSprintf)
	p0plusGlobal(t, set, "queryURL", p0plusStandInQueryURL)
	tmpl, cerr := set.GetTemplate("probe/globals.html")
	if cerr != nil {
		t.Fatalf("编译探针模板失败: %v", cerr)
	}
	defer func() {
		if r := recover(); r != nil {
			out, err = fmt.Sprintf("PANIC: %v", r), nil
		}
	}()
	var buf strings.Builder
	e := tmpl.Execute(&buf, nil, data)
	return buf.String(), e
}

func p0plusRender(t *testing.T, tpl string) (string, error) {
	t.Helper()
	return p0plusRenderData(t, tpl, nil)
}

func TestP0PlusGlobalsAreCallableInExpressions(t *testing.T) {
	for _, tc := range []struct {
		name string
		tpl  string
		want []string
	}{
		{
			name: "pageNumbers 结果直接 range",
			tpl:  `{{range _, n := pageNumbers(2, 3, 7)}}[{{n}}]{{end}}`,
			want: []string{"[1]", "[2]", "[3]"},
		},
		{
			name: "pageNumbers 先绑变量再取长度",
			tpl:  `{{xs := pageNumbers(1, 4, 7)}}len={{len(xs)}}`,
			want: []string{"len=4"},
		},
		{
			name: "pageNumbers 结果进 if 条件",
			tpl:  `{{if len(pageNumbers(1, 0, 7)) > 0}}HAS{{else}}NONE{{end}}`,
			want: []string{"NONE"},
		},
		{
			name: "sprintf 用真词条造句（字面量入参）",
			tpl:  `{{sprintf("共 %s 条，第 %s-%s 条", 120, 21, 40)}}`,
			want: []string{"共 120 条，第 21-40 条"},
		},
		{
			name: "sprintf 用结构体字段造句（真 int 入参）",
			tpl:  `{{pg := pager(.Pagination)}}{{sprintf("共 %s 条，第 %s-%s 条", pg.Total, pg.Start, pg.End)}}`,
			want: []string{"共 120 条，第 21-40 条"},
		},
		{

			name: "queryURL 无查询串的 base（文本位转义为 &amp;）",
			tpl:  `{{queryURL("/admin/mail", "page", 2, "limit", 20)}}`,
			want: []string{`/admin/mail?page=2&amp;limit=20`},
		},
		{
			name: "queryURL 带筛选的 base 用 & 续接（文本位转义为 &amp;）",
			tpl:  `{{queryURL("/admin/mail?type=in", "page", 3)}}`,
			want: []string{`/admin/mail?type=in&amp;page=3`},
		},
		{
			name: "queryURL 直接进属性（单参数）",
			tpl:  `<a href="{{queryURL("/admin/mail", "account_page", 2)}}">2</a>`,
			want: []string{`href="/admin/mail?account_page=2"`},
		},
		{
			name: "queryURL 直接进属性（多参数，属性位同样只转义一次）",
			tpl:  `<a href="{{queryURL("/admin/mail", "page", 2, "limit", 20)}}">2</a>`,
			want: []string{`href="/admin/mail?page=2&amp;limit=20"`},
		},
		{
			name: "queryURL 空 base 回空串",
			tpl:  `[{{queryURL("", "page", 2)}}]`,
			want: []string{"[]"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var data map[string]any
			if strings.Contains(tc.tpl, ".Pagination") {
				data = map[string]any{"Pagination": pagination.Page{Total: 120, Page: 2, Limit: 20}}
			}
			out, err := p0plusRenderData(t, tc.tpl, data)
			if err != nil {
				t.Fatalf("渲染失败: %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Fatalf("输出缺少 %q，实际: %q", w, out)
				}
			}
			if strings.Contains(out, "%!") {
				t.Fatalf("插值留下未消费的格式符: %q", out)
			}
		})
	}
}

func TestP0PlusSprintfNeedsStringifyStep(t *testing.T) {
	const pattern = "共 %s 条，第 %s-%s 条"

	literalArgs := []any{float64(120), float64(21), float64(40)}
	raw := fmt.Sprintf(pattern, literalArgs...)
	if !strings.Contains(raw, "%!s(float64=") {
		t.Fatalf("负对照失效：直接 Sprintf 竟然没坏，说明本结论的前提（字面量是 float64）已不成立: %q", raw)
	}
	vals := make([]any, 0, 3)
	for _, a := range []any{float64(120), float64(21), float64(40)} {
		vals = append(vals, stringify(a))
	}
	good := fmt.Sprintf(pattern, vals...)
	if good != "共 120 条，第 21-40 条" {
		t.Fatalf("stringify 后应得中文句，实际: %q", good)
	}

	out, err := p0plusRender(t, `{{sprintf("共 %s 条，第 %s-%s 条", 120, 21, 40)}}`)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != "共 120 条，第 21-40 条" {
		t.Fatalf("模板侧 sprintf 未产出期望句，实际: %q", out)
	}
}

func TestP0PlusEmptyAndMissingReturnsStayQuiet(t *testing.T) {
	out, err := p0plusRender(t, `{{range _, n := pageNumbers(1, 0, 7)}}[{{n}}]{{end}}<end>`)
	if err != nil {
		t.Fatalf("空窗口不该报错: %v", err)
	}
	if strings.Contains(out, "[") {
		t.Fatalf("空窗口不该渲染出任何页码，实际: %q", out)
	}

	out, err = p0plusRender(t, `{{queryURL("", "page", 1)}}|{{sprintf("")}}|`)
	if err != nil {
		t.Fatalf("空入参不该报错: %v", err)
	}
	if out != "||" {
		t.Fatalf("空入参应渲染成空串，实际: %q", out)
	}
}

func TestP0PlusSprintfHazardsAreDetectableByWarnPredicate(t *testing.T) {
	cases := map[string]string{
		"参数不足": `{{sprintf("共 %s 条，第 %s-%s 条", 1)}}`,
		"参数多余": `{{sprintf("共 %s 条", 1, 2)}}`,
	}
	for name, tpl := range cases {
		out, err := p0plusRender(t, tpl)
		if err != nil {
			t.Fatalf("%s: 渲染不该失败（错配是 Warn 级，不是 500）: %v", name, err)
		}
		if !strings.Contains(out, "%!") {
			t.Fatalf("%s: 期望出现 %%! 标记（Warn 判据的基础），实际: %q", name, out)
		}
	}
}

func TestP0PlusQueryURLMustNotPreEscape(t *testing.T) {
	got := p0plusStandInQueryURL("/admin/mail", "page", 2, "limit", 20)
	if !strings.Contains(got, "&") || strings.Contains(got, "&amp;") {
		t.Fatalf("Go 侧应返回未转义的 & ，实际: %q", got)
	}

	out, err := p0plusRender(t, `<a href="{{queryURL("/admin/mail", "page", 2, "limit", 20)}}">x</a>`)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if strings.Contains(out, "&amp;amp;") {
		t.Fatalf("属性位出现双重转义（Go 侧预转义 + Jet 再转义），实际: %q", out)
	}
	if !strings.Contains(out, `page=2&amp;limit=20`) {
		t.Fatalf("属性位应恰好转义一次，实际: %q", out)
	}
}

func TestP0PlusBareGlobalNameIsNotACall(t *testing.T) {
	out, err := p0plusRender(t, `bare=[{{pageNumbers}}] {{if pageNumbers}}ENTERED{{end}}`)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(out, "ENTERED") {
		t.Fatalf("裸名分支未进入 —— 说明本仓 Jet 行为已变，需重写结论与模板写法: %q", out)
	}
	if !strings.Contains(out, "bare=[0x") {
		t.Fatalf("裸名应渲染成函数地址，实际: %q", out)
	}

	out, err = p0plusRender(t, `{{if len(pageNumbers(1, 0, 7)) > 0}}ENTERED{{else}}EMPTY{{end}}`)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != "EMPTY" {
		t.Fatalf("空窗口必须走 else，实际: %q", out)
	}
}

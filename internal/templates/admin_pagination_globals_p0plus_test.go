package templates

// admin_pagination_globals_p0plus_test.go — 分页迁移 P0b+：**全局函数**在模板里的可用性（task-97 追加，lead 裁定 (a)）。
//
// 为什么单独一个文件、而不是改 P0b：P0b（`admin_pagination_probe_p0_test.go`）已过 Sol 限定复验，
// 它的 hash 是那次 PASS 的对象；补覆盖应该产生**新的评审单元**，而不是让已评审的产物悄悄变形。
//
// 要回答的问题：task-94 v2 让 partial 自己造信息行/按钮链接/页码选集，依赖 3 个**通用模板全局函数**
// （`pageNumbers`/`sprintf`/`queryURL`）。P0b 用的是内存 Set 且**不注入 globals**，所以它证明的是
// 「Jet 对 isset/字段/方法/插值/range 的语义」，**不含**「全局函数能在表达式、`{{if}}`、`{{range}}`
// 里被调用」——把前者当后者的旁证是不成立的推断，这里补上。
//
// 本文件不写生产代码（P1 才落地那 3 个函数）。替身的**签名与 task-94 计划逐字一致**，并且：
// `p0plusGlobal` 先用 `Set.LookupGlobal` 探测同名全局——**生产全局一旦存在（P1 落地后）就不再注册替身**，
// 本探针自动改测真实实现，绝不影子化（否则 P1 上线后这里会「绿得毫无意义」）。
//
// 已经踩到的两条 Jet 事实（funcs.go 头部也记着第一条）：
//   - **模板里的数字字面量是 float64**（`{{ typeOf(1000) }}` → float64），而结构体字段是真 int64/int。
//     所以 `sprintf` 这类「把值塞进 %s 词条」的函数必须过 `stringify`：不过它就会得到
//     `共 %!s(float64=120) 条`（见 TestP0PlusSprintfNeedsStringifyStep 的负对照）。
//   - **裸名字（无括号）不是调用**：`{{if pageNumbers}}` 恒为真（取到的是函数值）。与 P0b 对方法
//     的结论同一条：调用一律带括号。

import (
	"fmt"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/pagination"
)

// p0plusStandInPageNumbers 替身：签名与 task-94 计划的 `pageNumbers(page, pages, win int) []int` 一致。
// 真实窗口算法是 P1 的事（P1 会用 oracle 对照旧实现扫 pages=1..20），这里只验证「能不能调、能不能 range」。
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

// p0plusStandInSprintf 替身：与 task-94 计划一致 —— **每个值先过 stringify 再进 fmt.Sprintf**。
// 这一步不是装饰：模板字面量是 float64，直接 Sprintf 会把 %s 打成 `%!s(float64=120)`。
//
// 实现细节（P1 落地时必须照此写）：本包的 `stringify(v any) string` 吃**单个**值、返回 string，
// 没有「切片级 stringify」这种函数，所以 design 里那句 `fmt.Sprintf(pattern, stringify(args)...)`
// 只能逐个映射后展开，不能直接写。
func p0plusStandInSprintf(pattern string, args ...any) string {
	vals := make([]any, len(args))
	for i, a := range args {
		vals[i] = stringify(a)
	}
	return fmt.Sprintf(pattern, vals...)
}

// p0plusStandInQueryURL 替身：与 task-94 计划一致的「已知事实串成 href」语义
// （base 已含 `?` 用 `&`、空 base 回空串、值按 url.Values 口径转义、按调用顺序追加）。
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

// p0plusGlobal 注册替身，但**不影子化**生产全局：同名全局已存在时原样复用。
func p0plusGlobal(t *testing.T, set *jet.Set, name string, impl any) {
	t.Helper()
	if _, found := set.LookupGlobal(name); found {
		t.Logf("全局 %q 已由生产注册，探针直接测真实实现（不覆盖）", name)
		return
	}
	set.AddGlobal(name, impl)
}

// p0plusRenderData 渲染一段内联模板：生产同配置 + 生产 globals + 三个替身，并带上渲染数据。
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

// p0plusRender 无渲染数据的快捷方式。
func p0plusRender(t *testing.T, tpl string) (string, error) {
	t.Helper()
	return p0plusRenderData(t, tpl, nil)
}

// TestP0PlusGlobalsAreCallableInExpressions 三个计划中的全局函数必须能在 `{{if}}`/`{{range}}`/插值里调用。
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
			// 注意期望值里的 &amp;：Jet 会把插值结果做 HTML 转义，多参数 URL 在**文本位**
			// 渲染出来就是 &amp;（这正是 P0a golden 里冻结到的字节）。
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

// TestP0PlusSprintfNeedsStringifyStep 把「为什么必须过 stringify」变成可执行事实：
// 同一条词条、同一组入参，直接 fmt.Sprintf 得到 `%!s(float64=…)`，过 stringify 才得到中文句。
func TestP0PlusSprintfNeedsStringifyStep(t *testing.T) {
	const pattern = "共 %s 条，第 %s-%s 条"
	// 用切片展开调用：字面量在模板里就是 float64，这里复现同一形态（顺带绕开 vet 对
	// 「%s 收到 float64」的静态告警 —— 那个告警正是本用例要证明的现象）。
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
	// 同时确认模板侧走的就是这条链（替身/生产实现都要过 stringify）。
	out, err := p0plusRender(t, `{{sprintf("共 %s 条，第 %s-%s 条", 120, 21, 40)}}`)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != "共 120 条，第 21-40 条" {
		t.Fatalf("模板侧 sprintf 未产出期望句，实际: %q", out)
	}
}

// TestP0PlusEmptyAndMissingReturnsStayQuiet 全局函数返回空切片/空串时不得报错，也不得渲染出东西：
// 分页条在「窗口为空」时应该整块安静——这是 partial 里 `{{if len(...)}}` 之类守卫的依据。
func TestP0PlusEmptyAndMissingReturnsStayQuiet(t *testing.T) {
	out, err := p0plusRender(t, `{{range _, n := pageNumbers(1, 0, 7)}}[{{n}}]{{end}}<end>`)
	if err != nil {
		t.Fatalf("空窗口不该报错: %v", err)
	}
	if strings.Contains(out, "[") {
		t.Fatalf("空窗口不该渲染出任何页码，实际: %q", out)
	}
	// `sprintf("")`：模板侧零参数调用（真 partial 里不会这么写，但守卫必须不炸）。
	out, err = p0plusRender(t, `{{queryURL("", "page", 1)}}|{{sprintf("")}}|`)
	if err != nil {
		t.Fatalf("空入参不该报错: %v", err)
	}
	if out != "||" {
		t.Fatalf("空入参应渲染成空串，实际: %q", out)
	}
}

// TestP0PlusSprintfHazardsAreDetectableByWarnPredicate 参数个数与词条不匹配时，Go 的 Sprintf 会给出
// `%!s(MISSING)` / `%!(EXTRA …) 标记 —— task-94 计划让 `sprintf` 在这种结果上打 Warn。
// 这条把「Warn 的判据（结果含 %!）确实能覆盖两种错配」变成可执行事实，免得将来把判据写成别的形态。
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

// TestP0PlusQueryURLMustNotPreEscape Go 侧返回的 URL 必须是**未转义**的原文：Jet 会在渲染时转义一次。
// 若 P1 在 Go 里预先 `html.EscapeString`/`url.QueryEscape` 整个串，页面上就会看到 `&amp;amp;`——
// 这类双重转义不会报错、只会静默坏掉筛选链接，所以在这里钉死。
func TestP0PlusQueryURLMustNotPreEscape(t *testing.T) {
	got := p0plusStandInQueryURL("/admin/mail", "page", 2, "limit", 20)
	if !strings.Contains(got, "&") || strings.Contains(got, "&amp;") {
		t.Fatalf("Go 侧应返回未转义的 & ，实际: %q", got)
	}
	// 生产全局若已存在（P1 落地后），同一判据同样适用。
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

// TestP0PlusBareGlobalNameIsNotACall 与 P0b 对方法的结论同源：**全局函数裸名（无括号）也不是调用**。
// 常驻绊线：`{{if pageNumbers}}` 恒为真（取到函数值），谁把括号写丢了，分页条会在「窗口为空」时照样渲染。
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
	// 反面对照：带括号 + 真判据时，空窗口必须走 else 分支。
	out, err = p0plusRender(t, `{{if len(pageNumbers(1, 0, 7)) > 0}}ENTERED{{else}}EMPTY{{end}}`)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != "EMPTY" {
		t.Fatalf("空窗口必须走 else，实际: %q", out)
	}
}

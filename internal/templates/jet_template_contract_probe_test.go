package templates

// jet_template_contract_probe_test.go — task-155（P0-2）的 Jet 兼容层契约探针。
//
// 本文件只回答三件**实测**问题，为下一批 3600 处 `.["t"]` 调用点迁移定契约定稿：
//
//   A) `isset` 与 dd.txt 4.2 的 `hasVal` 语义矩阵逐格比对（每格：isset 结果 / hasVal 结果 / 是否一致）；
//   B) 「要不要新增 hasVal」有可执行判据（不是推理，见各用例的断言）；
//   C) 翻译函数 t/tf 的请求级注入通道（Set 级 vs per-render）与并发不串语言。
//
// 探针一律走**真实 Jet 渲染**（内存 Set + 生产 injectGlobals），不直接调 Go 函数：
// 「注册漏了 / Jet 对某种入参形状的处理与直觉不符」只有在渲染路径上才暴露（同 funcs_test.go 的立场）。
//
// 与既有用例的分工（先核对过，不重复劳动）：
//   - i18n_jet_test.go:TestJetSetGlobalIsProcessShared —— AddGlobal 是 Set 级共享（C 的前半）已钉；
//   - jet_missing_t_varform_test.go —— 缺 t 的变量形态/直调形态后果已钉；
//   - jet_index_and_call_test.go —— map 变量键取值、data 里的函数值可直调已钉；
//   - admin_pagination_probe_p0_test.go —— isset 把「类型化 nil 指针」当未设置（值语义那一格）已钉。
//
// 本文件补的是：矩阵的其余格 + 两种取值形态的对照 + per-render 变量通道 + 并发不串语言。

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

// jcHasValGlobal 是 dd.txt 4.2 `hasVal` 的探针化身：**生产 isNil（funcs.go:315）取反**。
//
// 为什么用生产的 isNil 而不是另写一份：本次要回答的是「isset 与 hasVal 是否一致」，
// 探针里再写第二份判据，量到的就是那份判据而不是结论（两份判据必然漂移）。
const jcHasValGlobal = "jcHasVal"

// jcInner / jcRow 用于「结构体字段」形态的守卫探针（product_row.go 那条改造动机的形状）。
type jcInner struct{ N int }

type jcRow struct {
	ComparePrice *float64
	Name         string
	Opts         []string
	NilPtr       *jcInner
}

// jcProbeSet 建探针 Set：生产配置 + 生产 injectGlobals + 探针 hasVal（只加探针名，不影子化生产名）。
func jcProbeSet(t *testing.T, files map[string]string) *jet.Set {
	t.Helper()
	set := memSet(t, files)
	injectGlobals(set)
	set.AddGlobal(jcHasValGlobal, func(v any) bool { return !isNil(v) })
	return set
}

// jcRender 渲染一小段模板；panic 也收敛成 error（缺键那几档要看的正是 error/panic 形态）。
func jcRender(t *testing.T, set *jet.Set, name string, data map[string]any) (out string, err error) {
	t.Helper()
	tmpl, err := set.GetTemplate(name)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	defer func() {
		if r := recover(); r != nil {
			out = buf.String()
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	if e := tmpl.Execute(&buf, nil, data); e != nil {
		return buf.String(), e
	}
	return buf.String(), nil
}

func jcBool(b bool) string {
	if b {
		return "T"
	}
	return "F"
}

// jcCell 是矩阵的一格。want 同时是 dd.txt 4.2 对 hasVal 的期望与本次实测 isset 的期望 ——
// 两者不一致就是「差异格」。
type jcCell struct {
	name  string
	value any
	want  bool
}

// jcMatrixCells 是 A 项的输入集合：dd.txt 4.2 的全部格子 + 三类 dd 未列但真实存在的可变 nil 种类。
func jcMatrixCells() []jcCell {
	var (
		nilFloat *float64
		nilRow   *jcRow
		nilSlice []string
		nilMap   map[string]any
		nilFunc  func()
		nilChan  chan int
		zero     = 0.0
	)
	return []jcCell{
		{"裸 nil（键存在、值为 nil 接口）", nil, false},
		{"typed nil *float64", nilFloat, false},
		{"typed nil *struct", nilRow, false},
		{"指向 0 的指针 *float64(&0.0)", &zero, true},
		{"数字 int 0", 0, true},
		{"数字 float64 0.0", 0.0, true},
		{"布尔 false", false, true},
		{"空字符串", "", true},
		{"非 nil 空 map", map[string]any{}, true},
		{"nil map（var m map[string]any）", nilMap, false},
		{"非空 map", map[string]any{"k": 1}, true},
		{"非 nil 空 slice", []string{}, true},
		{"nil slice（var s []string）", nilSlice, false},
		{"struct 零值", jcRow{}, true},
		{"非 nil 函数值", func() {}, true},
		{"typed nil func", nilFunc, false},
		{"typed nil chan", nilChan, false},
	}
}

// TestJetContractIssetVsHasValMatrix A 项：逐格钉住 isset（两种取值形态）/ hasVal / Go 侧 !isNil。
//
// 任一侧语义变了（升级 Jet、改 funcs.go 的 isNil）本用例变红。
func TestJetContractIssetVsHasValMatrix(t *testing.T) {
	const tpl = `field=[{{if isset(.X)}}T{{else}}F{{end}}] index=[{{if isset(.["X"])}}T{{else}}F{{end}}] hasVal=[{{jcHasVal(.X)}}]`
	set := jcProbeSet(t, map[string]string{"probe/matrix.html": tpl})

	for _, c := range jcMatrixCells() {
		t.Run(c.name, func(t *testing.T) {
			out, err := jcRender(t, set, "probe/matrix.html", map[string]any{"X": c.value})
			if err != nil {
				t.Fatalf("矩阵格渲染失败（isset 对任何非缺键输入都不该报错）: %v", err)
			}
			want := fmt.Sprintf("field=[%s] index=[%s] hasVal=[%v]", jcBool(c.want), jcBool(c.want), c.want)
			if out != want {
				t.Fatalf("格 %q 实测 %s，期望 %s（dd.txt 4.2）", c.name, out, want)
			}
			if got := !isNil(c.value); got != c.want {
				t.Fatalf("Go 侧 !isNil(%#v) = %v，dd.txt 4.2 期望 %v", c.value, got, c.want)
			}
			t.Logf("格 %q → %s", c.name, out)
		})
	}
}

// jcShapeTpl 是「守卫形态」探针模板：EXPR 由每个用例替换（模板名不放变量，Set 缓存按名字）。
const jcShapeTpl = "isset=[{{if isset(EXPR)}}T{{else}}F{{end}}] hasVal=[{{jcHasVal(EXPR)}}]"

// TestJetContractGuardShapeMatrix isset/hasVal 在**两种取值形态**上的对照表：
// 结构体字段（字段恒存在，问的是「能不能为 nil 且当前是不是 nil」）与 map 键（问的是「键在不在」）。
//
// 这张表是迁移分档的依据：products.html:103 那类 `{{if isset(o.LabelKey)}}` 依赖的是 **map 缺键** 这一档
// （见 admin/product/products.html:100-103 的注释与 service/product_row_options_test.go 的判据），
// 而 thematic_settings.html:84 那类结构体字段上的 isset 恒为真，真正起作用的是包里那句 `{{if f.Invalid}}`。
func TestJetContractGuardShapeMatrix(t *testing.T) {
	var zero = 0.0
	for _, tc := range []struct {
		name string
		expr string
		data map[string]any
		want bool
	}{
		{"struct 字段：nil 指针", `.Row.ComparePrice`, map[string]any{"Row": jcRow{}}, false},
		{"struct 字段：非 nil 指针（指向 0）", `.Row.ComparePrice`, map[string]any{"Row": jcRow{ComparePrice: &zero}}, true},
		{"struct 字段：空串", `.Row.Name`, map[string]any{"Row": jcRow{}}, true},
		{"struct 字段：nil slice", `.Row.Opts`, map[string]any{"Row": jcRow{}}, false},
		{"struct 字段：非 nil 空 slice", `.Row.Opts`, map[string]any{"Row": jcRow{Opts: []string{}}}, true},
		{"struct 字段：嵌套 nil 指针", `.Row.NilPtr`, map[string]any{"Row": jcRow{}}, false},
		{"map 键：存在且为空串", `.["K"]`, map[string]any{"K": ""}, true},
		{"map 键：存在且为 nil 指针", `.["K"]`, map[string]any{"K": (*float64)(nil)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.ReplaceAll(jcShapeTpl, "EXPR", tc.expr)
			set := jcProbeSet(t, map[string]string{"probe/shape.html": src})
			out, err := jcRender(t, set, "probe/shape.html", tc.data)
			if err != nil {
				t.Fatalf("渲染失败: %v", err)
			}
			want := fmt.Sprintf("isset=[%s] hasVal=[%v]", jcBool(tc.want), tc.want)
			if out != want {
				t.Fatalf("实测 %s，期望 %s", out, want)
			}
			t.Logf("%s（%s）→ %s", tc.name, tc.expr, out)
		})
	}
}

// TestJetContractMissingKeyIsTheDifferenceCell A/B 项的关键差异格：**键不存在**。
//
// 结论（本用例钉住）：缺键那一档 isset 与 hasVal 不等价 ——
//   - `isset(.Missing)` 是**语法层守卫**（Jet 的 isSet 直接查节点、不求值，eval.go:741-785），返回 F 且不报错；
//   - `hasVal(.Missing)` 的实参必须先求值，而缺键求值本身就是渲染错误/panic。
//
// 因此 hasVal 不能顶替 isset：全站 488 处 isset 里占多数的是「键在不在」这类守卫
// （.Statuses / .Err / .LoadFailed / .FormEcho …），换掉它们等于把静默降级改成 500。
func TestJetContractMissingKeyIsTheDifferenceCell(t *testing.T) {
	set := jcProbeSet(t, map[string]string{
		"probe/missing.html":      `issetField=[{{if isset(.Missing)}}T{{else}}F{{end}}] issetIndex=[{{if isset(.["Missing"])}}T{{else}}F{{end}}]`,
		"probe/hasval_field.html": `[{{jcHasVal(.Missing)}}]`,
		"probe/hasval_index.html": `[{{jcHasVal(.["Missing"])}}]`,
	})
	out, err := jcRender(t, set, "probe/missing.html", map[string]any{})
	if err != nil {
		t.Fatalf("缺键的 isset 必须静默给 F（这是 488 处守卫的前提），实际报错: %v", err)
	}
	if out != "issetField=[F] issetIndex=[F]" {
		t.Fatalf("缺键 isset 期望两个形态全 F，实际 %q", out)
	}
	t.Logf("缺键 isset：%s（err=nil）", out)

	for _, c := range []struct{ form, tpl string }{
		{".Missing", "probe/hasval_field.html"},
		{`.["Missing"]`, "probe/hasval_index.html"},
	} {
		t.Run("hasVal"+c.form, func(t *testing.T) {
			got, err := jcRender(t, set, c.tpl, map[string]any{})
			t.Logf("hasVal(%s) 缺键 → out=%q err=%v", c.form, got, err)
			if err == nil && strings.Contains(got, "false") {
				t.Fatalf("hasVal(%s) 在缺键上静默给了 false —— 与「缺键 ≠ 有值」的结论冲突，需重写契约", c.form)
			}
			if err == nil {
				t.Logf("hasVal(%s) 缺键未报错，输出 %q（非 false 形态），记录在案", c.form, got)
			}
		})
	}
}

// TestJetContractHasValIfEverRegisteredMustEqualNotIsNil 前向闸门。
//
// 本次结论是「不新增 hasVal」（见 docs/rules/jet-template-contract.md）。
// 这条守的是将来：**若下一批真在生产里注册了 hasVal，它必须逐格等于 !isNil**，否则 red ——
// 冗余两份判据的漂移在这里第一次发生时就暴露，而不是等某个页面少一行文案。
func TestJetContractHasValIfEverRegisteredMustEqualNotIsNil(t *testing.T) {
	set := memSet(t, map[string]string{"probe/x.html": "x"})
	injectGlobals(set)

	raw, ok := set.LookupGlobal("hasVal")
	if !ok {
		t.Logf("生产未注册 hasVal（本次定稿：复用 isset + isNil）—— 本用例作为前向闸门待命")
		return
	}
	fn, ok := raw.(func(any) bool)
	if !ok {
		t.Fatalf("生产的 hasVal 类型是 %T，不是 func(any) bool —— 语义无法对账", raw)
	}
	for _, c := range jcMatrixCells() {
		if got := fn(c.value); got != c.want {
			t.Fatalf("生产 hasVal(%#v) = %v，dd.txt 4.2 期望 %v（与 isNil 分叉）", c.value, got, c.want)
		}
	}
	t.Logf("生产 hasVal 与 !isNil 在 %d 格上一致", len(jcMatrixCells()))
}

// TestJetContractTranslatorMustNotBeASetGlobal C 项（闸门）：语言/翻译器**不得**是 Set 级全局。
//
// 语言是请求级的，Set 是进程级单例（NewJetHTMLRender 启动时建、所有请求共享，
// 见 i18n_jet_test.go:TestJetSetGlobalIsProcessShared 的实测）。任何 `Set.AddGlobal("t", …)`
// 都会让并发请求互相串词，而 -race 只报数据竞争、报不出「串的是别人的语言」。
func TestJetContractTranslatorMustNotBeASetGlobal(t *testing.T) {
	set := memSet(t, map[string]string{"probe/x.html": "x"})
	injectGlobals(set)

	for _, name := range []string{"t", "tf", "tr", "currentLocale", "currentTranslator", "lang"} {
		if v, found := set.LookupGlobal(name); found {
			t.Fatalf("Set 级全局 %q 被注册（%T）—— 请求级状态不能走 Set.AddGlobal（见 adminui/injectI18n 的注释）", name, v)
		}
	}
	for _, name := range []string{"money", "pageNumbers", "siteDateTime"} {
		if _, found := set.LookupGlobal(name); !found {
			t.Fatalf("生产全局 %q 未注册 —— injectGlobals 没生效，本用例前半不成立", name)
		}
	}
}

// TestJetContractPerRenderVarsChannelReachesInclude C 项：Jet 的**每渲染变量通道**存在且被 include 继承。
//
// API 依据（go.mod: github.com/CloudyKit/jet/v6 v6.3.2）：
//   - exec.go:55 `func (t *Template) Execute(w io.Writer, variables VarMap, data interface{})`
//     —— 第一个参数就是 per-render 变量表；resolve() 的查找顺序是
//     「当前/父作用域 variables → Set 全局 → Jet 内置变量」（eval.go:179-215），variables 先于全局；
//   - eval.go:88-93 newScope() 挂 parent 链、eval.go:604-631 executeInclude 也走 newScope()
//     → include / import / yield 都继承这条链。
//
// 现状：本仓驱动把第一个参数传 nil（pkg/view/driver/jet/jet.go:62 `t.Execute(&buf, nil, vars)`），
// 且 `driver.Driver.Render(name string, vars map[string]any)` 只有一个数据通道 —— 要用这条通道必须改驱动，
// 属下一批（本批不得改，补丁见 docs/rules/jet-template-contract.md）。
func TestJetContractPerRenderVarsChannelReachesInclude(t *testing.T) {
	set := memSet(t, map[string]string{
		"probe/parent.html": `P[{{include "child.html"}}]`,
		"probe/child.html":  `{{t("k", "兜底")}}`,
	})
	injectGlobals(set)

	tmpl, err := set.GetTemplate("probe/parent.html")
	if err != nil {
		t.Fatalf("取父模板失败: %v", err)
	}
	vars := jet.VarMap{}
	vars.Set("t", func(key, fallback string) string { return "L:" + key })

	var buf strings.Builder
	if err := tmpl.Execute(&buf, vars, nil); err != nil {
		t.Fatalf("per-render 变量通道渲染失败: %v", err)
	}
	if buf.String() != "P[L:k]" {
		t.Fatalf("每渲染变量表必须被 include 继承，期望 P[L:k]，实际 %q", buf.String())
	}
	t.Logf("per-render vars → include 继承：%q", buf.String())

	if _, found := set.LookupGlobal("t"); found {
		t.Fatal("探针 Set 里出现了全局 t —— 变量通道被污染，结论不成立")
	}
}

// TestJetContractAssignmentFormDependsOnChannel C 项：CLAUDE.md 第 2 条那句限制**只针对 data 形态**。
//
// 已知（internal/templates/CLAUDE.md:148-150）：`.["t"](...)` 不能出现在赋值右侧/单目表达式里，
// 报 `unexpected node type … in unary expression`（Jet eval.go:1186），所以现在一律先
// `{{tr := .["t"]}}` 再 `tr(...)`。本用例把两种通道各自的可用形态量出来：
//   - data 通道：赋值形态必须报错（钉住既有结论；Jet 升级后若放开会 red → 重评模板写法）；
//   - per-render 变量通道：赋值形态是否可用（决定下一批能否写 `{{x := t(...)}}`）。
func TestJetContractAssignmentFormDependsOnChannel(t *testing.T) {
	set := jcProbeSet(t, map[string]string{
		"probe/direct.html":      `[{{ .["t"]("k", "兜底") }}]`,
		"probe/assign_data.html": `{{x := .["t"]("k", "兜底")}}[{{x}}]`,
	})
	tf := func(key, fallback string) string { return "L:" + key }

	if out, err := jcRender(t, set, "probe/direct.html", map[string]any{"t": tf}); err != nil || out != "[L:k]" {
		t.Fatalf("data 通道直调形态应可用，实际 out=%q err=%v", out, err)
	}

	_, err := jcRender(t, set, "probe/assign_data.html", map[string]any{"t": tf})
	if err == nil {
		t.Fatalf("data 通道的赋值形态竟然通过了 —— CLAUDE.md 第 2 条失效，需重评 3600 处写法")
	}
	if !strings.Contains(err.Error(), "unary expression") {
		t.Fatalf("期望 unary expression 报错，实际 %v", err)
	}
	t.Logf("data 通道赋值形态：%v", err)

	vset := memSet(t, map[string]string{"probe/assign_vars.html": `{{x := t("k", "兜底")}}[{{x}}]`})
	injectGlobals(vset)
	vtmpl, err := vset.GetTemplate("probe/assign_vars.html")
	if err != nil {
		t.Logf("per-render 通道赋值形态编译失败（形态不可用）：%v", err)
		return
	}
	vars := jet.VarMap{}
	vars.Set("t", tf)
	var buf strings.Builder
	execErr := vtmpl.Execute(&buf, vars, nil)
	t.Logf("per-render 通道赋值形态：out=%q err=%v", buf.String(), execErr)
}

// TestJetContractConcurrentPerRequestTranslatorIsolated C 项（并发闸门）：
// 一个共享 Set + 按请求注入的 t，在并发渲染下**语言不串用**。
//
// 判据的写法：每格渲染结果必须**逐字节**等于自己语言的期望串（含语言标记）——
// 不断言「至少出现过两种语言」，那种断言在串用时照样绿。跑法（-race 一起）：
//
//	go test ./internal/templates/ -run TestJetContractConcurrentPerRequestTranslatorIsolated -race -count=1
func TestJetContractConcurrentPerRequestTranslatorIsolated(t *testing.T) {
	set := jcProbeSet(t, map[string]string{
		"probe/lang.html": `{{tr := .["t"]}}[{{tr("k", "兜底")}}|{{tr("m", "兜底")}}|{{ .["lang"] }}]`,
	})
	tmpl, err := set.GetTemplate("probe/lang.html")
	if err != nil {
		t.Fatalf("取模板失败: %v", err)
	}

	langs := []string{"zh-CN", "en-US", "ja-JP", "de-DE"}
	const (
		goroutines = 8
		perRoutine = 40
	)
	errs := make(chan string, goroutines*len(langs))
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		for _, lang := range langs {
			wg.Add(1)
			go func(lang string) {
				defer wg.Done()
				// 每个「请求」拿到的是一份独立的 data（含自己的 t 闭包）—— 与 adminui.Prepare 的形态一致。
				data := map[string]any{
					"lang": lang,
					"t": func(key, fallback string) string {
						return lang + ":" + key
					},
				}
				want := "[" + lang + ":k|" + lang + ":m|" + lang + "]"
				for n := 0; n < perRoutine; n++ {
					var buf strings.Builder
					if err := tmpl.Execute(&buf, nil, data); err != nil {
						errs <- fmt.Sprintf("lang=%s 渲染失败: %v", lang, err)
						return
					}
					if buf.String() != want {
						errs <- fmt.Sprintf("语言串用：lang=%s want %q got %q", lang, want, buf.String())
						return
					}
				}
			}(lang)
		}
	}
	wg.Wait()
	close(errs)
	bad := 0
	for e := range errs {
		bad++
		if bad <= 5 {
			t.Errorf("%s", e)
		}
	}
	if bad > 0 {
		t.Fatalf("并发下语言串用/渲染失败 %d 次（data 注入通道不成立）", bad)
	}
	t.Logf("并发渲染 %d 次（%d 语言 × %d 协程 × %d 轮）无串用",
		goroutines*len(langs)*perRoutine, len(langs), goroutines, perRoutine)
}

// TestJetContractMapArgAndAssignmentShapes 把三条**迁移形态**一次量清（决定下一批怎么写）：
//
//  1. 现状 workaround：`{{tr := .["t"]}}` 之后 `{{x := tr("k","f")}}` —— 赋值位置用变量即可绕开限制；
//  2. `map("t", .["t"], …)` 传参形态（现网 11 处，如 admin/partials/date_filter.html:19 的用法）：
//     片段内部 `{{ .["t"](...) }}` 可用（Jet 只禁 map 参数位置写 .["t"]，不禁调用位置）；
//  3. per-render 变量通道下的等价写法：`map("t", t, …)`（参数位置写标识符），片段内 `t(...)` 直调可用。
//
// 第 3 条是「改驱动」之后能省掉的东西：map 参数位置不再需要 workaround，组件也不必自己取词。
func TestJetContractMapArgAndAssignmentShapes(t *testing.T) {
	const (
		tplAssignVars = `{{tr := .["t"]}}{{x := tr("k", "兜底")}}[{{x}}]`
		tplMapArgData = `{{include "child_map.html" map("t", .["t"], "p", "P")}}`
		tplMapArgVars = `{{include "child_map.html" map("t", t, "p", "P")}}`
		tplChildMap   = `{{ .["t"]("k", "兜底") }}|{{ .["p"] }}`
	)
	tf := func(key, fallback string) string { return "L:" + key }

	// 1) 现状 workaround：赋值位置用变量
	set1 := jcProbeSet(t, map[string]string{"probe/a.html": tplAssignVars})
	if out, err := jcRender(t, set1, "probe/a.html", map[string]any{"t": tf}); err != nil || out != "[L:k]" {
		t.Fatalf("tr 变量形态在赋值位置应可用，实际 out=%q err=%v", out, err)
	} else {
		t.Logf("① data 通道 + tr 变量赋值位置：out=%q err=nil", out)
	}

	// 2) 现网 map 传参形态（data 通道）
	set2 := jcProbeSet(t, map[string]string{"probe/p.html": tplMapArgData, "probe/child_map.html": tplChildMap})
	out2, err2 := jcRender(t, set2, "probe/p.html", map[string]any{"t": tf})
	if err2 != nil || out2 != "L:k|P" {
		t.Fatalf("map 传参形态（data 通道）应可用，实际 out=%q err=%v", out2, err2)
	}
	t.Logf("② map 传参形态（data 通道 .[\"t\"]）→ out=%q err=nil", out2)

	// 3) per-render 变量通道下的 map 传参（参数位置写标识符）
	set3 := memSet(t, map[string]string{"probe/p.html": tplMapArgVars, "probe/child_map.html": tplChildMap})
	injectGlobals(set3)
	tmpl3, err := set3.GetTemplate("probe/p.html")
	if err != nil {
		t.Fatalf("取模板失败: %v", err)
	}
	vars := jet.VarMap{}
	vars.Set("t", tf)
	var buf strings.Builder
	execErr := tmpl3.Execute(&buf, vars, nil)
	if execErr != nil || buf.String() != "L:k|P" {
		t.Fatalf("map 传参（参数位写标识符）在变量通道下应可用，实际 out=%q err=%v", buf.String(), execErr)
	}
	t.Logf("③ per-render vars + map 传参（参数位写标识符）→ out=%q err=nil", buf.String())
}

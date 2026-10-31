package templates

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

const jcHasValGlobal = "jcHasVal"

type jcInner struct{ N int }

type jcRow struct {
	ComparePrice *float64
	Name         string
	Opts         []string
	NilPtr       *jcInner
}

func jcProbeSet(t *testing.T, files map[string]string) *jet.Set {
	t.Helper()
	set := memSet(t, files)
	injectGlobals(set)
	set.AddGlobal(jcHasValGlobal, func(v any) bool { return !isNil(v) })
	return set
}

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

type jcCell struct {
	name  string
	value any
	want  bool
}

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

const jcShapeTpl = "isset=[{{if isset(EXPR)}}T{{else}}F{{end}}] hasVal=[{{jcHasVal(EXPR)}}]"

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

func TestJetContractMapArgAndAssignmentShapes(t *testing.T) {
	const (
		tplAssignVars = `{{tr := .["t"]}}{{x := tr("k", "兜底")}}[{{x}}]`
		tplMapArgData = `{{include "child_map.html" map("t", .["t"], "p", "P")}}`
		tplMapArgVars = `{{include "child_map.html" map("t", t, "p", "P")}}`
		tplChildMap   = `{{ .["t"]("k", "兜底") }}|{{ .["p"] }}`
	)
	tf := func(key, fallback string) string { return "L:" + key }

	set1 := jcProbeSet(t, map[string]string{"probe/a.html": tplAssignVars})
	if out, err := jcRender(t, set1, "probe/a.html", map[string]any{"t": tf}); err != nil || out != "[L:k]" {
		t.Fatalf("tr 变量形态在赋值位置应可用，实际 out=%q err=%v", out, err)
	} else {
		t.Logf("① data 通道 + tr 变量赋值位置：out=%q err=nil", out)
	}

	set2 := jcProbeSet(t, map[string]string{"probe/p.html": tplMapArgData, "probe/child_map.html": tplChildMap})
	out2, err2 := jcRender(t, set2, "probe/p.html", map[string]any{"t": tf})
	if err2 != nil || out2 != "L:k|P" {
		t.Fatalf("map 传参形态（data 通道）应可用，实际 out=%q err=%v", out2, err2)
	}
	t.Logf("② map 传参形态（data 通道 .[\"t\"]）→ out=%q err=nil", out2)

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

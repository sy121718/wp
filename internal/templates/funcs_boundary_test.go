package templates

// funcs_boundary_test.go — B6「未知枚举、非法 JSON 和 HTML 转义有测试」的判据。
//
// 覆盖 funcs.go 注册的全局函数（injectGlobals 的 AddGlobal 清单）在**三类边界**下的实际行为，
// 一律走真实 Jet 渲染（内存 Set + 生产 injectGlobals）：本文件记录的不是「函数返回什么」，
// 而是「页面上出现什么字节」——「Jet 拒绝裸 nil 字面量做实参」「插值默认转义一次」这类行为
// 只在渲染路径上成立（同 jet_template_contract_probe_test.go 的立场）。
//
// 三类边界：
//
//	A) HTML 转义 —— {{ }} 与 {{|raw}} 的差异，map 与 struct 两种上下文；
//	B) 未知枚举取值 —— 变化率方向、未知枚举传给数值函数；
//	C) 非法 / 越界入参 —— 非法 JSON、null JSON、越界整数字面量。
//
// 姿态是**把当前实际行为钉住**，不是提出期望：与直觉不符的地方写在各用例注释里
//（typed nil 指针直出会渲染成 <nil>、非法 JSON 渲染层不校验），报告里另列清单外发现。
//
// 与既有用例的分工（逐条核对过，不重复劳动）：
//   - funcs_test.go —— 各函数的取值矩阵（含 currencyPrefix 各档、percent 小数位越界）；
//   - funcs_pagination_test.go 与 admin_pagination_globals_p0plus_test.go —— queryURL / sprintf 的
//     「恰好转义一次」与「坏格式串留 %! 痕迹」；
//   - jet_template_contract_probe_test.go（task-155）—— isset/hasVal 矩阵与 t 注入通道。
//
// 本文件补的是上面都没覆盖的：raw 对照、struct 上下文、percentChange / percentChangeDir、
// json.RawMessage 直出、typed nil 直出、越界字面量。

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/CloudyKit/jet/v6"
)

// bpRow 是 struct 上下文的形状（字段与 dto 里的可空列同形：string 与指针）。
type bpRow struct {
	Name   string
	NilPtr *int
}

// bpSet 自带一份渲染通道，不复用 task-155 探针的 jcProbeSet / jcRender：那些 helper 属于探针
// 文件，随迁移推进可能被删，而边界判据要能独立存活。
func bpSet(t *testing.T, files map[string]string) *jet.Set {
	t.Helper()
	l := jet.NewInMemLoader()
	for name, src := range files {
		l.Set(name, src)
	}
	set := jet.NewSet(l, jet.WithTemplateNameExtensions([]string{"", ".jet"}))
	injectGlobals(set)
	return set
}

// bpRender 渲染一段模板，并把 panic 收敛成 error —— 边界用例要看的正是「失败长什么样」
// （半截输出 + 可定位的错误，还是静默给一个看起来成立的值）。
func bpRender(t *testing.T, set *jet.Set, name string, data map[string]any) (out string, err error) {
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

// ==========================================================================
// A) HTML 转义：{{ }} 与 {{|raw}} 的差异，map 与 struct 两种上下文。
// ==========================================================================

func TestBoundaryEscapeMapAndStructContexts(t *testing.T) {
	// map 取键与 struct 取字段在转义上**没有差别**（都走同一条插值路径），而 |raw 一律关掉转义。
	// 双引号被转义成 &#34; 而不是 &quot; —— Jet 的转义表长这样，写死期望值是为了让升级 Jet
	// 导致的字节变化在这里显形（属性位由浏览器两种都能解析，但字节会变，golden 会红）。
	set := bpSet(t, map[string]string{
		"esc.jet": `MAP=[{{ .["v"] }}] MAPRAW=[{{ .["v"]|raw }}] STRUCT=[{{ .Row.Name }}] STRUCTRAW=[{{ .Row.Name|raw }}]`,
	})
	data := map[string]any{"v": `<b>x</b> & "q"`, "Row": bpRow{Name: "<i>n</i>&x"}}
	out, err := bpRender(t, set, "esc.jet", data)
	if err != nil {
		t.Fatalf("转义路径不该报错，实际 %v", err)
	}
	want := `MAP=[&lt;b&gt;x&lt;/b&gt; &amp; &#34;q&#34;] MAPRAW=[<b>x</b> & "q"] STRUCT=[&lt;i&gt;n&lt;/i&gt;&amp;x] STRUCTRAW=[<i>n</i>&x]`
	if out != want {
		t.Fatalf("转义形态与上下文无关、raw 关掉转义：\n实际 %q\n期望 %q", out, want)
	}
}

func TestBoundaryRawDisablesEscapingOnURLText(t *testing.T) {
	// queryURL 的「函数侧不预转义」已由 funcs_pagination_test.go 与
	// admin_pagination_globals_p0plus_test.go 钉住；这里补的是同一事实的 raw 对照：
	// 默认形态给出合法 HTML 的 &amp;，raw 形态给出裸 &（只有调用点明确接受时才可用）。
	set := bpSet(t, map[string]string{
		"u.jet": `A=[{{ queryURL("/a?x=1", "k", "a b&c") }}] B=[{{ queryURL("/a?x=1", "k", "a b&c")|raw }}]`,
	})
	out, err := bpRender(t, set, "u.jet", nil)
	if err != nil {
		t.Fatalf("不预设转义的返回值不该报错，实际 %v", err)
	}
	want := `A=[/a?x=1&amp;k=a+b%26c] B=[/a?x=1&k=a+b%26c]`
	if out != want {
		t.Fatalf("实际 %q，期望 %q", out, want)
	}
	if strings.Contains(out, "&amp;amp;") {
		t.Fatalf("出现双重转义（函数侧预转义 + 渲染转义）：%q", out)
	}
}

func TestBoundaryTypedNilPointerRendersNilLiteral(t *testing.T) {
	// **直出 typed nil 指针会渲染成 <nil>**（再被 HTML 转义成 &lt;nil&gt;）——页面上多出一行
	// <nil>，不报错、也没人发现。经函数收敛（stringify 那条路）才是空串，这正是 stringify
	// 存在的理由（funcs.go 的注释：dto 的可空列是 *time.Time / *uint64 / *int64）。
	set := bpSet(t, map[string]string{
		"n.jet": `DIRECT=[{{ .Row.NilPtr }}] VIA_FN=[{{ formValue(.Echo, "x", .Row.NilPtr) }}]`,
	})
	var nilEcho url.Values
	data := map[string]any{"Row": bpRow{NilPtr: nil}, "Echo": nilEcho}
	out, err := bpRender(t, set, "n.jet", data)
	if err != nil {
		t.Fatalf("不该报错，实际 %v", err)
	}
	want := `DIRECT=[&lt;nil&gt;] VIA_FN=[]`
	if out != want {
		t.Fatalf("实际 %q，期望 %q（直出给 <nil>、过 stringify 给空串）", out, want)
	}
}

func TestBoundaryNilLiteralIsNotAValidArgument(t *testing.T) {
	// 裸 nil 字面量在 Jet 里不是「没有值」，而是**参数无效**：渲染当场失败（不是静默给空串）。
	// 推论：模板里判空只能用 {{if .X}} 或数据里传 typed nil，不能靠给函数传 nil。
	cases := []struct {
		name string
		tpl  string
	}{
		{"dateTime(nil)", `{{ dateTime(nil) }}`},
		{"percentChange(nil, 1)", `{{ percentChange(nil, 1) }}`},
		{"formValue(nil, x, fb)", `{{ formValue(nil, "x", "fb") }}`},
		{"dateTime(字符串时间)", `{{ dateTime("2026-01-02 15:04") }}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var set *jet.Set
			if tc.name == "dateTime(字符串时间)" {
				// 字面量时间在模板里同样不可用：时间必须由数据传进来（time.Time / utils.JSONTime）。
				set = bpSet(t, map[string]string{"c.jet": `T=[` + tc.tpl + `]`})
				out, err := bpRender(t, set, "c.jet", nil)
				if err == nil {
					t.Fatalf("字面量时间必须失败，实际渲染出 %q", out)
				}
				if out != "T=[" {
					t.Fatalf("失败点之前的字节应保留（半截输出），实际 %q", out)
				}
				return
			}
			set = bpSet(t, map[string]string{"c.jet": `T=[` + tc.tpl + `]`})
			out, err := bpRender(t, set, "c.jet", nil)
			if err == nil {
				t.Fatalf("裸 nil 字面量必须失败，实际渲染出 %q", out)
			}
			if !strings.Contains(err.Error(), "is not a valid value") {
				t.Fatalf("错误信息应指出实参无效，实际 %v", err)
			}
			if out != "T=[" {
				t.Fatalf("失败点之前的字节应保留（半截输出），实际 %q", out)
			}
		})
	}
}

func TestBoundaryTypedNilArgumentIsFine(t *testing.T) {
	// 对照：Go 侧传进来的 typed nil 是正常入参 —— dateTime 给空串、percentChange 给空串、
	// formValue 回落到初始值。三者都是「没有值」的正确表达。
	set := bpSet(t, map[string]string{
		"t.jet": `TIME=[{{ dateTime(.T) }}] PCT=[{{ percentChange(.N, 1) }}] FORM=[{{ formValue(.Echo, "x", "fb") }}] IF=[{{ if .T }}has{{ else }}none{{ end }}]`,
	})
	var nilTime *time.Time
	var nilFloat *float64
	var nilEcho url.Values
	data := map[string]any{"T": nilTime, "N": nilFloat, "Echo": nilEcho}
	out, err := bpRender(t, set, "t.jet", data)
	if err != nil {
		t.Fatalf("typed nil 是正常入参，不该报错，实际 %v", err)
	}
	want := `TIME=[] PCT=[] FORM=[fb] IF=[none]`
	if out != want {
		t.Fatalf("实际 %q，期望 %q", out, want)
	}
}

// ==========================================================================
// B) 未知枚举取值：能落地的取值要精确，落不了地的取值要响亮失败。
// ==========================================================================

func TestBoundaryPercentChangeDirection(t *testing.T) {
	// percentChangeDir 的四个取值是模板挂涨跌配色的判据。**恰好 0 是 flat 而不是 down**：
	// 0.0% 画一个向下箭头是错的；无基期（typed nil）是 none 而不是 flat —— 「上期一单没卖」
	// 和「持平」不是同一件事。
	set := bpSet(t, map[string]string{
		"d.jet": `UP=[{{ percentChangeDir(1.5) }}] DOWN=[{{ percentChangeDir(-1.5) }}] ZERO=[{{ percentChangeDir(0) }}] NOBASE=[{{ percentChangeDir(.N) }}]`,
	})
	var nilFloat *float64
	out, err := bpRender(t, set, "d.jet", map[string]any{"N": nilFloat})
	if err != nil {
		t.Fatalf("不该报错，实际 %v", err)
	}
	want := `UP=[up] DOWN=[down] ZERO=[flat] NOBASE=[none]`
	if out != want {
		t.Fatalf("实际 %q，期望 %q", out, want)
	}
}

func TestBoundaryPercentChangeSign(t *testing.T) {
	// 与 percent 的差异就是符号：变化率场景里「涨还是跌」本身是信息。
	// 无基期回空串而不是 +0.0%（后者看起来像「持平」）。
	set := bpSet(t, map[string]string{
		"p.jet": `UP=[{{ percentChange(1.5, 1) }}] DOWN=[{{ percentChange(-3, 1) }}] ZERO=[{{ percentChange(0, 1) }}] NOBASE=[{{ percentChange(.N, 1) }}]`,
	})
	var nilFloat *float64
	out, err := bpRender(t, set, "p.jet", map[string]any{"N": nilFloat})
	if err != nil {
		t.Fatalf("不该报错，实际 %v", err)
	}
	want := `UP=[+1.5%] DOWN=[-3.0%] ZERO=[0.0%] NOBASE=[]`
	if out != want {
		t.Fatalf("实际 %q，期望 %q", out, want)
	}
}

func TestBoundaryUnknownEnumPassedToNumericFnFailsLoudly(t *testing.T) {
	// 本批最要紧的一格：把枚举串（例如 status）误传给数值函数时，**必须响亮失败**。
	// 静默按 0 处理会渲染成 flat / 0.0%，而那看起来是一个成立的结论 —— 这类失效没人会去查。
	set := bpSet(t, map[string]string{
		"e.jet": `DIR=[{{ percentChangeDir("published") }}]`,
	})
	out, err := bpRender(t, set, "e.jet", nil)
	if err == nil {
		t.Fatalf("未知枚举必须失败，实际渲染出 %q", out)
	}
	if !strings.Contains(err.Error(), "模板函数 percentChangeDir：期望整数，实际 string（值 published）") {
		t.Fatalf("错误信息要能直接定位（函数名 + 实际类型 + 实际值），实际 %v", err)
	}
	if out != "DIR=[" {
		t.Fatalf("失败发生在插值处，之前的字节保留（半截输出不会 swap 进页面），实际 %q", out)
	}
}

func TestBoundaryCurrencyPrefixUnknownEnum(t *testing.T) {
	// 币种前缀的过渡规则（真源是 sys_dict 的 symbol 列）：空 / CNY / RMB 给 ¥，其余给
	// 「大写代码 + 空格」。这里只补既有 funcs_test.go 未覆盖的两格：**全空白**与**未知小写代码**。
	set := bpSet(t, map[string]string{
		"c.jet": `BLANK=[{{ currencyPrefix("   ") }}] UNKNOWN=[{{ currencyPrefix("jpy") }}]`,
	})
	out, err := bpRender(t, set, "c.jet", nil)
	if err != nil {
		t.Fatalf("不该报错，实际 %v", err)
	}
	want := `BLANK=[¥] UNKNOWN=[JPY ]`
	if out != want {
		t.Fatalf("实际 %q，期望 %q", out, want)
	}
}

// ==========================================================================
// C) 非法 / 越界入参：渲染层不校验，责任在 Go 侧；越界的字面量在解析期就挡住。
// ==========================================================================

func TestBoundaryJSONRawMessageIsRenderedUnvalidated(t *testing.T) {
	// json.RawMessage 对模板就是一个字符串：**渲染层不校验 JSON** —— 合法与非法都照样吐到
	// 页面上（合法时引号被转义成 &#34;）。把「非法 JSON 静默降级」的责任放在 Go 侧是刻意的：
	// product service 的 PageDraftTitle / EntityPlainText 各自把非法 JSON 收敛成空串
	// （判据在 internal/module/product/service/product_seo_score_input_test.go）。
	// 若哪天这里开始报错，就是渲染层多了一层没人要求的校验 —— 本用例会红。
	set := bpSet(t, map[string]string{
		"j.jet": `OK=[{{ .Doc }}] BAD=[{{ .Bad }}] NULL=[{{ .NilDoc }}] OKRAW=[{{ .Doc|raw }}] BADRAW=[{{ .Bad|raw }}]`,
	})
	data := map[string]any{
		"Doc":    json.RawMessage(`{"a":1}`),
		"Bad":    json.RawMessage(`{"a":`),
		"NilDoc": json.RawMessage(nil),
	}
	out, err := bpRender(t, set, "j.jet", data)
	if err != nil {
		t.Fatalf("渲染层不该对 JSON 内容做校验（解析责任在 Go 侧），实际报错 %v", err)
	}
	want := `OK=[{&#34;a&#34;:1}] BAD=[{&#34;a&#34;:] NULL=[] OKRAW=[{"a":1}] BADRAW=[{"a":]`
	if out != want {
		t.Fatalf("实际 %q，期望 %q", out, want)
	}
}

func TestBoundaryOverflowingIntegerLiteralFailsBeforeRender(t *testing.T) {
	// 超 int64 的整数字面量在**解析期**失败（不是运行时）：输出为空，错误里带模板名与行号。
	// 判据的意义是「坏字面量不该渲染出一半页面」—— 页面上半截表格比 500 更难查。
	set := bpSet(t, map[string]string{
		"big.jet": `BIG=[{{ money(100000000000000000000, "Y") }}]`,
	})
	out, err := bpRender(t, set, "big.jet", nil)
	if err == nil {
		t.Fatalf("越界整数字面量必须失败，实际渲染出 %q", out)
	}
	if !strings.Contains(err.Error(), "integer overflow") {
		t.Fatalf("错误应指出字面量溢出，实际 %v", err)
	}
	if out != "" {
		t.Fatalf("解析期失败时没有任何字节产出，实际 %q", out)
	}
}

func TestBoundaryOverflowingFloatLiteralFailsAtRender(t *testing.T) {
	// 浮点字面量（1e30）能过解析，栽在函数的范围检查上：错误信息带函数名与实际值。
	set := bpSet(t, map[string]string{
		"f.jet": `F=[{{ money(1e30, "Y") }}]`,
	})
	out, err := bpRender(t, set, "f.jet", nil)
	if err == nil {
		t.Fatalf("越界浮点入参必须失败，实际渲染出 %q", out)
	}
	if !strings.Contains(err.Error(), "模板函数 money：期望int64 范围内的整数，实际 float64（值 1e+30）") {
		t.Fatalf("错误信息要能直接定位，实际 %v", err)
	}
	if out != "F=[" {
		t.Fatalf("半截输出应止于失败点，实际 %q", out)
	}
}

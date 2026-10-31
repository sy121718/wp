package templates

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/CloudyKit/jet/v6"
)

type bpRow struct {
	Name   string
	NilPtr *int
}

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

func TestBoundaryEscapeMapAndStructContexts(t *testing.T) {

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

func TestBoundaryPercentChangeDirection(t *testing.T) {

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

func TestBoundaryJSONRawMessageIsRenderedUnvalidated(t *testing.T) {

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
	want := `OK=[{&#34;a&#34;:1}] BAD=[{&#34;a&#34;:] NULL=[null] OKRAW=[{"a":1}] BADRAW=[{"a":]`
	if out != want {
		t.Fatalf("实际 %q，期望 %q", out, want)
	}
}

func TestBoundaryOverflowingIntegerLiteralFailsBeforeRender(t *testing.T) {

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

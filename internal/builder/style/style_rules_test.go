package style

// style_rules_test.go — 表达力补齐的回归（docs/06-E-plugin-authoring.md §3）：
//   - 触屏治理分派：hover / hover-none / active 各自进专用样式桶；
//   - CSS 变量导出（vars）：控件值 → --name，供插件包 assets/*.css 消费；
//   - 容器/主题查询（queries）：size / theme / local 三桶分层输出；
//   - 新增白名单的拒绝路径与专用桶/断点的互斥校验。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// compileWithQueries 编译并同时返回常规 CSS 与容器查询 CSS（两者分开输出）。
func compileWithQueries(t *testing.T, nodeID string, props map[string]any, s *Schema) (css, queries string) {
	t.Helper()
	b := &core.CSSBuckets{}
	if err := Compile(nodeID, props, s, b); err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	return b.String(), b.ContainerQueryCSS()
}

// TestCompilePseudoDedicatedBuckets 伪类分派：hover 包 (hover: hover)、
// hover-none 进 (hover: none)、active 不包媒体查询且排在 hover 之后。
func TestCompilePseudoDedicatedBuckets(t *testing.T) {
	s := &Schema{Rules: []Rule{
		{Pseudo: "hover", Decls: [][2]string{{"transform", "translateY(-2px)"}}},
		{Pseudo: "hover-none", Decls: [][2]string{{"border-width", "2px"}}},
		{Pseudo: "active", Decls: [][2]string{{"transform", "scale(.98)"}}},
		{Pseudo: "focus", Decls: [][2]string{{"outline", "2px solid #2563eb"}}},
		{Pseudo: "first-child", Decls: [][2]string{{"margin-top", "0"}}},
	}}
	got := compileOnce(t, "h-1", nil, s)
	if !strings.Contains(got, "@media (hover: hover) {") || !strings.Contains(got, ".sky-c-h-1:hover {") {
		t.Fatalf("hover 未进 (hover: hover) 桶:\n%s", got)
	}
	if !strings.Contains(got, "@media (hover: none) {") || !strings.Contains(got, "border-width: 2px") {
		t.Fatalf("hover-none 未进 (hover: none) 桶:\n%s", got)
	}
	if strings.Contains(got, "@media (hover: hover) {\n  .sky-c-h-1:active") {
		t.Fatalf("active 不应包 hover 媒体查询（触屏上会完全失去按压反馈）:\n%s", got)
	}
	if strings.Index(got, ".sky-c-h-1:active") < strings.Index(got, "@media (hover: hover)") {
		t.Fatalf("active 应排在 hover 块之后（同时成立时按压态胜出）:\n%s", got)
	}
	if !strings.Contains(got, ".sky-c-h-1:focus {") || !strings.Contains(got, ".sky-c-h-1:first-child {") {
		t.Fatalf("普通伪类与结构伪类应拼在选择器尾部:\n%s", got)
	}
}

// TestCompileVars 变量导出：控件值 → CSS 变量，未设置的不导出，顺序固定。
func TestCompileVars(t *testing.T) {
	s := &Schema{Rules: []Rule{{
		Decls: [][2]string{{"display", "flex"}},
		Vars: []VarBinding{
			{Name: "card-accent", From: "accent"},
			{Name: "card-gap", From: "gap"},
			{Name: "card-unset", From: "missing"},
		},
		Bindings: []Binding{{Prop: "gap", From: "gap"}},
	}}}
	got := compileOnce(t, "v-1", map[string]any{"accent": "#2563eb", "gap": "12px"}, s)
	want := ".sky-c-v-1 {\n  display: flex;\n  --card-accent: #2563eb;\n  --card-gap: 12px;\n  gap: 12px;\n}"
	if got != want {
		t.Fatalf("变量导出不符:\ngot:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "card-unset") {
		t.Fatalf("未设置的控件不应导出变量:\n%s", got)
	}
}

// TestCompileVarValueInjection 变量值的防御深度（与绑定值同一道值白名单）。
func TestCompileVarValueInjection(t *testing.T) {
	s := &Schema{Rules: []Rule{{Vars: []VarBinding{{Name: "accent", From: "accent"}}}}}
	for _, evil := range []string{
		"red;background:url(https://evil.com/x)",
		"} .sky-c-other { display:none",
		"url(//evil.com/a.gif)",
	} {
		b := &core.CSSBuckets{}
		if err := Compile("v-1", map[string]any{"accent": evil}, s, b); err == nil {
			t.Fatalf("恶意变量值 %q 应返回错误，实际输出: %s", evil, b.String())
		}
	}
}

// TestCompileQueries 容器/主题查询：三桶分别包进 sky-auto/sky-theme/sky-local 层，层序固定。
func TestCompileQueries(t *testing.T) {
	s := &Schema{Rules: []Rule{{
		Decls: [][2]string{{"display", "block"}},
		Queries: []Query{
			{Kind: "size", Condition: "(width >= 480px)", Decls: [][2]string{{"display", "flex"}}},
			{Kind: "theme", Container: "sky-theme", Prop: "--sky-density", Value: "compact", Decls: [][2]string{{"padding", "8px"}}},
			{Kind: "local", Container: "sky-card", Prop: "--sky-layout", Value: "wide", Decls: [][2]string{{"flex-direction", "row"}}},
		},
	}}}
	_, cq := compileWithQueries(t, "q-1", nil, s)
	for _, want := range []string{
		"@container (width >= 480px) {",
		"@container sky-theme style(--sky-density: compact) {",
		"@container sky-card style(--sky-layout: wide) {",
		"@layer sky-auto {",
		"@layer sky-theme {",
		"@layer sky-local {",
		".sky-c-q-1 {",
	} {
		if !strings.Contains(cq, want) {
			t.Fatalf("容器查询输出缺少 %q:\n%s", want, cq)
		}
	}
	auto := strings.Index(cq, "@layer sky-auto {")
	theme := strings.Index(cq, "@layer sky-theme {")
	local := strings.Index(cq, "@layer sky-local {")
	if !(auto < theme && theme < local) {
		t.Fatalf("层序应为 sky-auto < sky-theme < sky-local（优先级由层序决定）:\n%s", cq)
	}
}

// TestValidateRejectsDedicatedPseudoWithBreakpoints 专用桶没有断点维度，互斥拒绝。
func TestValidateRejectsDedicatedPseudoWithBreakpoints(t *testing.T) {
	for _, p := range []string{"hover", "hover-none", "active"} {
		s := &Schema{Rules: []Rule{{
			Pseudo:      p,
			Breakpoints: map[string][][2]string{core.BreakpointMobile: {{"gap", "4px"}}},
		}}}
		if err := s.Validate(); err == nil {
			t.Fatalf("pseudo %q 与 breakpoints 同时声明应被拒绝（否则断点声明被静默丢弃）", p)
		}
	}
	ok := &Schema{Rules: []Rule{{
		Pseudo:      "focus",
		Breakpoints: map[string][][2]string{core.BreakpointMobile: {{"gap", "4px"}}},
	}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("普通伪类带断点应合法: %v", err)
	}
}

// TestValidateRejectsBadVarsAndQueries 变量与查询声明的白名单拒绝路径。
func TestValidateRejectsBadVarsAndQueries(t *testing.T) {
	cases := []struct {
		name string
		s    *Schema
	}{
		{"变量名含空格", &Schema{Rules: []Rule{{Vars: []VarBinding{{Name: "Card Accent", From: "a"}}}}}},
		{"变量名带前导 --", &Schema{Rules: []Rule{{Vars: []VarBinding{{Name: "--accent", From: "a"}}}}}},
		{"变量缺 from", &Schema{Rules: []Rule{{Vars: []VarBinding{{Name: "accent"}}}}}},
		{"查询 kind 未知", &Schema{Rules: []Rule{{Queries: []Query{{Kind: "wide", Decls: [][2]string{{"display", "block"}}}}}}}},
		{"尺寸条件带组合式", &Schema{Rules: []Rule{{Queries: []Query{{Kind: "size", Condition: "(width >= 480px) and (height > 0)", Decls: [][2]string{{"display", "block"}}}}}}}},
		{"尺寸条件花括号逃逸", &Schema{Rules: []Rule{{Queries: []Query{{Kind: "size", Condition: "} .sky-c-other { display: none", Decls: [][2]string{{"display", "block"}}}}}}}},
		{"尺寸查询混入 container 字段", &Schema{Rules: []Rule{{Queries: []Query{{Kind: "size", Condition: "(width >= 480px)", Container: "sky-x", Decls: [][2]string{{"display", "block"}}}}}}}},
		{"主题查询属性非自定义属性", &Schema{Rules: []Rule{{Queries: []Query{{Kind: "theme", Container: "sky-theme", Prop: "color", Value: "red", Decls: [][2]string{{"padding", "8px"}}}}}}}},
		{"主题查询值注入", &Schema{Rules: []Rule{{Queries: []Query{{Kind: "theme", Container: "sky-theme", Prop: "--sky-density", Value: "compact;}", Decls: [][2]string{{"padding", "8px"}}}}}}}},
		{"查询缺 decls", &Schema{Rules: []Rule{{Queries: []Query{{Kind: "local", Container: "sky-card", Prop: "--sky-layout", Value: "wide"}}}}}},
		{"查询声明属性不在白名单", &Schema{Rules: []Rule{{Queries: []Query{{Kind: "size", Condition: "(width >= 480px)", Decls: [][2]string{{"behavior", "url(#x)"}}}}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.s.Validate(); err == nil {
				t.Fatalf("应拒绝非法声明")
			}
		})
	}
}

// TestValidateAcceptsNewPrimitives 新增原语的合法宣告通过校验。
func TestValidateAcceptsNewPrimitives(t *testing.T) {
	s := &Schema{Rules: []Rule{{
		Pseudo: "hover-none",
		Vars:   []VarBinding{{Name: "card-accent", From: "accent"}},
		Queries: []Query{
			{Kind: "size", Condition: "(width >= 480px)", Decls: [][2]string{{"display", "flex"}}},
			{Kind: "theme", Container: "sky-theme", Prop: "--sky-density", Value: "compact", Decls: [][2]string{{"padding", "8px"}}},
		},
	}}}
	if err := s.Validate(); err != nil {
		t.Fatalf("合法的新原语声明应通过: %v", err)
	}
}

// TestCompileNewPrimitivesDeterministic 新增原语同样保持确定性。
func TestCompileNewPrimitivesDeterministic(t *testing.T) {
	s := &Schema{Rules: []Rule{{
		Pseudo: "hover",
		Vars:   []VarBinding{{Name: "card-accent", From: "accent"}},
		Decls:  [][2]string{{"display", "flex"}},
		Queries: []Query{
			{Kind: "size", Condition: "(width >= 480px)", Decls: [][2]string{{"gap", "16px"}}},
			{Kind: "theme", Container: "sky-theme", Prop: "--sky-density", Value: "compact", Decls: [][2]string{{"padding", "8px"}}},
		},
		Breakpoints: nil,
	}}}
	props := map[string]any{"accent": "#111"}
	css, cq := compileWithQueries(t, "d-2", props, s)
	for i := 0; i < 20; i++ {
		c2, q2 := compileWithQueries(t, "d-2", props, s)
		if c2 != css || q2 != cq {
			t.Fatalf("第 %d 次编译不确定", i)
		}
	}
}

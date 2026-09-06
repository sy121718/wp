package style

// style 引擎测试：表驱动编译语义 + 确定性（同一输入两次编译字节相同）+
// 注入防护（选择器/属性名/值三面）+ fuzz（随机 props 不 panic 且确定）。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// compileOnce 便捷封装：编译并返回 CSS 字符串。
func compileOnce(t *testing.T, nodeID string, props map[string]any, s *Schema) string {
	t.Helper()
	b := &core.CSSBuckets{}
	if err := Compile(nodeID, props, s, b); err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	return b.String()
}

// TestCompileBasicBindings 属性绑定与静态声明：选择器、声明拼接、跳空值。
func TestCompileBasicBindings(t *testing.T) {
	s := &Schema{Rules: []Rule{{
		Decls: [][2]string{
			{"display", "flex"},
			{"gap", "16px"},
		},
		Bindings: []Binding{
			{Prop: "background", From: "bgColor"},
			{Prop: "border-radius", From: "radius"},
			{Prop: "color", From: "unset"}, // 控件未设置：跳过
		},
	}}}
	got := compileOnce(t, "card-1", map[string]any{"bgColor": "#fff", "radius": "12px"}, s)
	want := `.wp-c-card-1 {
  display: flex;
  gap: 16px;
  background: #fff;
  border-radius: 12px;
}`
	if got != want {
		t.Fatalf("编译结果不符:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestCompileVariantWhen 条件变体：命中/不命中/值不匹配。
func TestCompileVariantWhen(t *testing.T) {
	s := &Schema{Rules: []Rule{
		{When: "style=raised", Decls: [][2]string{{"box-shadow", "0 8px 24px rgba(0,0,0,.12)"}}},
	}}
	// 命中。
	if got := compileOnce(t, "c-1", map[string]any{"style": "raised"}, s); !strings.Contains(got, "box-shadow") {
		t.Fatalf("条件命中应输出声明: %s", got)
	}
	// 不命中：无输出。
	if got := compileOnce(t, "c-1", map[string]any{"style": "flat"}, s); got != "" {
		t.Fatalf("条件不命中不应有输出: %s", got)
	}
	// 键缺失：不命中。
	if got := compileOnce(t, "c-1", map[string]any{}, s); got != "" {
		t.Fatalf("键缺失应视为不命中: %s", got)
	}
}

// TestCompilePseudoAndTarget 伪类与子元素选择器（受控拼装）。
func TestCompilePseudoAndTarget(t *testing.T) {
	s := &Schema{Rules: []Rule{
		{Pseudo: "hover", Bindings: []Binding{{Prop: "transform", From: "lift", Prefix: "translateY(", Suffix: ")"}}},
		{Target: ".badge", Decls: [][2]string{{"font-size", "12px"}}},
		{Target: ".head .title", Pseudo: "focus", Decls: [][2]string{{"outline", "2px solid #2563eb"}}},
	}}
	got := compileOnce(t, "b-1", map[string]any{"lift": "-2px"}, s)
	for _, sel := range []string{".wp-c-b-1:hover", ".wp-c-b-1 .badge", ".wp-c-b-1 .head .title:focus"} {
		if !strings.Contains(got, sel+" {") {
			t.Fatalf("缺少选择器 %q:\n%s", sel, got)
		}
	}
	if !strings.Contains(got, "transform: translateY(-2px)") {
		t.Fatalf("前后缀绑定拼接错误:\n%s", got)
	}
}

// TestCompileResponsive 断点覆盖：tablet/mobile 进对应媒体查询。
func TestCompileResponsive(t *testing.T) {
	s := &Schema{Rules: []Rule{{
		Bindings: []Binding{{Prop: "padding", From: "pad"}},
		Breakpoints: map[string][][2]string{
			core.BreakpointTablet: {{"padding", "24px"}},
			core.BreakpointMobile: {{"padding", "16px"}},
		},
	}}}
	got := compileOnce(t, "r-1", map[string]any{"pad": "32px"}, s)
	if !strings.Contains(got, "padding: 32px") {
		t.Fatalf("缺少主断点声明:\n%s", got)
	}
	if !strings.Contains(got, "@media (max-width: 1024px)") || !strings.Contains(got, "padding: 24px") {
		t.Fatalf("缺少 tablet 覆盖:\n%s", got)
	}
	if !strings.Contains(got, "@media (max-width: 767px)") || !strings.Contains(got, "padding: 16px") {
		t.Fatalf("缺少 mobile 覆盖:\n%s", got)
	}
}

// TestCompilePropValueNormalization props 值归一：number → 字符串（16 而非 16.0）。
func TestCompilePropValueNormalization(t *testing.T) {
	s := &Schema{Rules: []Rule{{
		Bindings: []Binding{{Prop: "gap", From: "n"}, {Prop: "opacity", From: "f"}},
	}}}
	got := compileOnce(t, "n-1", map[string]any{"n": float64(16), "f": 0.5}, s)
	if !strings.Contains(got, "gap: 16;") || !strings.Contains(got, "opacity: 0.5;") {
		t.Fatalf("数值归一错误:\n%s", got)
	}
}

// TestCompileDeterministic 确定性：同一 schema + props 两次编译字节相同。
func TestCompileDeterministic(t *testing.T) {
	s := &Schema{Rules: []Rule{
		{Decls: [][2]string{{"display", "grid"}}, Bindings: []Binding{{Prop: "gap", From: "gap"}}},
		{When: "v=x", Target: ".item", Pseudo: "hover", Decls: [][2]string{{"opacity", ".8"}}},
	}}
	props := map[string]any{"gap": "8px", "v": "x"}
	a := compileOnce(t, "d-1", props, s)
	for i := 0; i < 20; i++ {
		if b := compileOnce(t, "d-1", props, s); b != a {
			t.Fatalf("第 %d 次编译不确定:\n%s\nvs\n%s", i, a, b)
		}
	}
}

// TestCompileUnsafeBindingValue 防御深度：绑定值注入（分号/外联 url）返回错误。
func TestCompileUnsafeBindingValue(t *testing.T) {
	s := &Schema{Rules: []Rule{{
		Bindings: []Binding{{Prop: "background", From: "color"}},
	}}}
	for _, evil := range []string{
		"red;background:url(https://evil.com/x)", // 分号 + 外联
		"url(//evil.com/a.gif)",                  // 协议相对外联
		"} .wp-c-other { display:none",           // 花括号逃逸
	} {
		b := &core.CSSBuckets{}
		if err := Compile("x-1", map[string]any{"color": evil}, s, b); err == nil {
			t.Fatalf("恶意值 %q 应返回错误，实际输出: %s", evil, b.String())
		}
	}
}

// TestValidateRejectsBadSchema schema 校验拒绝：属性名/选择器/伪类/断点/静态值。
func TestValidateRejectsBadSchema(t *testing.T) {
	cases := []struct {
		name string
		s    *Schema
	}{
		{"属性名不在白名单", &Schema{Rules: []Rule{{Decls: [][2]string{{"behavior", "url(#default#time2)"}}}}}},
		{"选择器注入", &Schema{Rules: []Rule{{Target: ".a, .wp-c-other"}}}},
		{"任意选择器字符串", &Schema{Rules: []Rule{{Target: "div *"}}}},
		{"伪类注入", &Schema{Rules: []Rule{{Pseudo: "hover) .x:not("}}}},
		{"静态值分号注入", &Schema{Rules: []Rule{{Decls: [][2]string{{"color", "red;z-index:9"}}}}}},
		{"非法断点", &Schema{Rules: []Rule{{Breakpoints: map[string][][2]string{"wide": {{"gap", "1px"}}}}}}},
		{"条件格式错误", &Schema{Rules: []Rule{{When: "style"}}}},
		{"绑定缺 from", &Schema{Rules: []Rule{{Bindings: []Binding{{Prop: "color"}}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.s.Validate(); err == nil {
				t.Fatalf("应拒绝非法 schema")
			}
		})
	}
}

// TestValidateAcceptsLegalSchema 合法 schema 通过校验。
func TestValidateAcceptsLegalSchema(t *testing.T) {
	s := &Schema{Rules: []Rule{
		{
			Target: ".badge", Pseudo: "hover", When: "style=raised",
			Decls:    [][2]string{{"box-shadow", "0 1px 3px rgba(0,0,0,.12)"}},
			Bindings: []Binding{{Prop: "transform", From: "lift", Prefix: "translateY(", Suffix: ")"}},
			Breakpoints: map[string][][2]string{
				core.BreakpointMobile: {{"font-size", "12px"}},
			},
		},
	}}
	if err := s.Validate(); err != nil {
		t.Fatalf("合法 schema 应通过: %v", err)
	}
}

// TestIsSafeProp 属性白名单边界。
func TestIsSafeProp(t *testing.T) {
	for _, p := range []string{"display", "border-radius", "transform", "grid-template-columns"} {
		if !IsSafeProp(p) {
			t.Fatalf("安全属性 %q 应在白名单", p)
		}
	}
	for _, p := range []string{"behavior", "binding", "content", "", "background-image;", "margin;"} {
		if IsSafeProp(p) {
			t.Fatalf("危险/非法属性 %q 不应在白名单", p)
		}
	}
}

// FuzzCompile 随机 props 不 panic、输出确定、恶意模式不进产物。
func FuzzCompile(f *testing.F) {
	f.Add("n-1", "red", "style", "raised")
	f.Add("n-1", "url(//x.com/a)", "a", "b")
	f.Add("n-1", "} .x {", "k", "v")
	f.Fuzz(func(t *testing.T, nodeID, val, key, enum string) {
		s := &Schema{Rules: []Rule{
			{Bindings: []Binding{{Prop: "color", From: key}}},
			{When: enum + "=" + enum, Decls: [][2]string{{"opacity", ".5"}}},
		}}
		props := map[string]any{key: val, enum: enum}
		b1 := &core.CSSBuckets{}
		if err := Compile(nodeID, props, s, b1); err != nil {
			return // 校验拒绝是合法路径
		}
		out := b1.String()
		// 产物每条声明行形如 "  prop: value;"——值部分（冒号后、终结分号前）
		// 不得含分号/花括号/外联 url（注入特征）。声明终结分号是合法格式化。
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if !strings.Contains(line, ":") || strings.HasSuffix(line, "{") || line == "}" {
				continue // 选择器行/媒体查询行/空行
			}
			value := line[strings.Index(line, ":")+1:]
			value = strings.TrimSuffix(strings.TrimSpace(value), ";")
			if strings.ContainsAny(value, ";{}") || strings.Contains(strings.ToLower(value), "url(http") || strings.Contains(strings.ToLower(value), "url(//") {
				t.Fatalf("声明值含注入特征 %q:\n%s", value, out)
			}
		}
		// 确定性。
		b2 := &core.CSSBuckets{}
		if err := Compile(nodeID, props, s, b2); err != nil {
			t.Fatalf("第二次编译意外失败: %v", err)
		}
		if b2.String() != out {
			t.Fatalf("编译不确定")
		}
	})
}

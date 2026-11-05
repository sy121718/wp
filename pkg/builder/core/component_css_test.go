package core

import (
	"strings"
	"testing"
)

func TestComponentCSSSubstitutesVars(t *testing.T) {
	var b CSSBuckets
	src := "& { width: {{width}}; background: {{color}}; }"
	if err := ApplyComponentCSSTmpl(&b, ".x", src, map[string]string{"width": "40px", "color": "#f00"}); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out := b.String()
	for _, want := range []string{".x {", "width: 40px", "background: #f00"} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
}

func TestComponentCSSTmplEmptyVarDropsDecl(t *testing.T) {
	var b CSSBuckets
	src := "& { display: flex; width: {{width}}; color: red; }"
	if err := ApplyComponentCSSTmpl(&b, ".x", src, map[string]string{"width": ""}); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out := b.String()
	if strings.Contains(out, "width") {
		t.Errorf("空变量应省略整条声明，产物里仍有 width:\n%s", out)
	}
	if !strings.Contains(out, "display: flex") || !strings.Contains(out, "color: red") {
		t.Errorf("同块内其余声明应当保留:\n%s", out)
	}
}

func TestComponentCSSIfSegments(t *testing.T) {
	const src = "& {\n" +
		"  display: block;\n" +
		"  @if solid\n" +
		"  background: {{color}};\n" +
		"  color: #fff;\n" +
		"  @endif\n" +
		"  @if outline\n" +
		"  color: {{color}};\n" +
		"  border: 1px solid {{color}};\n" +
		"  @endif\n" +
		"}"
	const outlineDecl = "border: 1px solid #000"
	cases := []struct {
		name           string
		solid, outline string
		want, notWant  []string
	}{
		{"solid 支命中", "1", "", []string{"background: #000", "color: #fff"}, []string{outlineDecl}},
		{"outline 支命中", "", "1", []string{outlineDecl}, []string{"background: #000"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b CSSBuckets
			vars := map[string]string{"solid": c.solid, "outline": c.outline, "color": "#000"}
			if err := ApplyComponentCSSTmpl(&b, ".x", src, vars); err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			out := b.String()
			for _, want := range c.want {
				if !strings.Contains(out, want) {
					t.Errorf("产物缺少 %q\n%s", want, out)
				}
			}
			for _, nw := range c.notWant {
				if strings.Contains(out, nw) {
					t.Errorf("未命中支的声明漏进产物 %q\n%s", nw, out)
				}
			}
			if n := strings.Count(out, ".x {"); n != 1 {
				t.Errorf("条件段应合并进同一条规则，实际 %d 条:\n%s", n, out)
			}
		})
	}
}

func TestComponentCSSNestedIf(t *testing.T) {
	const src = "& {\n" +
		"  display: block;\n" +
		"  @if outer\n" +
		"  color: red;\n" +
		"  @if inner\n" +
		"  color: blue;\n" +
		"  @endif\n" +
		"  @endif\n" +
		"}"
	var b CSSBuckets
	if err := ApplyComponentCSSTmpl(&b, ".x", src, map[string]string{"outer": "1", "inner": ""}); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "color: red") || strings.Contains(out, "color: blue") {
		t.Errorf("嵌套条件段判定错误:\n%s", out)
	}
}

func TestComponentCSSVarStrictness(t *testing.T) {
	cases := []struct {
		name string
		src  string
		vars map[string]string
	}{
		{"引用未提供的变量", "& { width: {{missing}}; }", map[string]string{"width": "1px"}},
		{"提供了但源里没用", "& { width: {{width}}; }", map[string]string{"width": "1px", "unused": "x"}},
		{"@if 引用未提供的变量", "& {\n  @if nope\n  color: red;\n  @endif\n}", map[string]string{}},
		{"无变量源里出现占位", "& { width: {{width}}; }", nil},
		{"占位没有闭合", "& { width: {{width; }", map[string]string{"width": "1px"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b CSSBuckets
			if err := ApplyComponentCSSTmpl(&b, ".x", c.src, c.vars); err == nil {
				t.Errorf("应当报错但通过了：src=%q vars=%v", c.src, c.vars)
			}
		})
	}
}

func TestComponentCSSOrphanEndif(t *testing.T) {
	var b CSSBuckets
	if err := ApplyComponentCSSTmpl(&b, ".x", "& {\n  @endif\n  color: red;\n}", map[string]string{}); err == nil {
		t.Error("孤儿 @endif 应当报错")
	}
	var b2 CSSBuckets
	if err := ApplyComponentCSSTmpl(&b2, ".x", "& {\n  @if a\n  color: red;\n}", map[string]string{"a": "1"}); err == nil {
		t.Error("未闭合的 @if 应当报错")
	}
}

func TestComponentCSSVarsInsideMedia(t *testing.T) {
	const src = "& { height: {{h_desktop}}; }\n" +
		"@media (max-width: 1024px) {\n  & {\n    height: {{h_tablet}};\n    @if tablet_extra\n    color: red;\n    @endif\n  }\n}\n" +
		"@hover & { color: {{hover_color}}; }"
	var b CSSBuckets
	vars := map[string]string{"h_desktop": "80px", "h_tablet": "", "tablet_extra": "1", "hover_color": "#0f0"}
	if err := ApplyComponentCSSTmpl(&b, ".x", src, vars); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "height: 80px") {
		t.Errorf("桌面桶缺少声明:\n%s", out)
	}
	if !strings.Contains(out, "color: #0f0") {
		t.Errorf("hover 桶缺少声明:\n%s", out)
	}

	if strings.Count(out, "height:") != 1 {
		t.Errorf("tablet 高度为空却进了产物:\n%s", out)
	}

	if !strings.Contains(out, "color: red") {
		t.Errorf("断点块内的条件段未生效:\n%s", out)
	}
}

func TestComponentCSSRuleLevelIf(t *testing.T) {
	const src = "& .a {\n  color: red;\n}\n" +
		"@if drift\n" +
		"& .b {\n  color: blue;\n}\n" +
		"@keyframes sky-x {\n  from { opacity: 0 }\n  to { opacity: 1 }\n}\n" +
		"@endif\n"

	var on CSSBuckets
	if err := ApplyComponentCSSTmpl(&on, ".x", src, map[string]string{"drift": "1"}); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out := on.String()
	for _, want := range []string{"color: blue", "@keyframes sky-x"} {
		if !strings.Contains(out, want) {
			t.Errorf("条件为真时缺少 %q\n%s", want, out)
		}
	}

	var off CSSBuckets
	if err := ApplyComponentCSSTmpl(&off, ".x", src, map[string]string{"drift": ""}); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out2 := off.String()
	for _, nw := range []string{"color: blue", "@keyframes sky-x"} {
		if strings.Contains(out2, nw) {
			t.Errorf("条件为假时不该产出 %q\n%s", nw, out2)
		}
	}
	if !strings.Contains(out2, "color: red") {
		t.Errorf("条件块外的规则应当保留\n%s", out2)
	}
}

func TestComponentCSSKeyframes(t *testing.T) {
	const src = "@keyframes sky-sd-drift {\n  from { transform: translateX(0) }\n  to { transform: translateX(-60px) }\n}\n"
	var b CSSBuckets
	if err := ApplyComponentCSS(&b, ".x", src); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	got := b.String()

	var want CSSBuckets
	want.AddKeyframesDecls("sky-sd-drift", []string{
		"from { transform: translateX(0) }",
		"to { transform: translateX(-60px) }",
	})
	if got != want.String() {
		t.Errorf("两条路径的关键帧产物不一致\n@keyframes 写法:\n%s\nAddKeyframesDecls 写法:\n%s", got, want.String())
	}
}

func TestComponentCSSSelectorShapes(t *testing.T) {
	const src = "@global .sub:has(+ {{scope}}) {\n  color: #000;\n}\n" +
		"& .one { display: block; }\n"
	var b CSSBuckets
	if err := ApplyComponentCSSTmpl(&b, ".sky-c-t", src, map[string]string{"scope": ".sky-c-t"}); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		".sub:has(+ .sky-c-t) {",
		".sky-c-t .one {\n  display: block;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
}

func TestComponentCSSEach(t *testing.T) {
	const src = "@each tab in tabs\n&:has({{tab.radio}}:checked) .panel[data-index=\"{{tab.index}}\"] {\n  display: block;\n}\n@endfor\n"
	lists := map[string][]map[string]string{
		"tabs": {
			{"radio": "#r0", "index": "0"},
			{"radio": "#r1", "index": "1"},
		},
	}
	var b CSSBuckets
	if err := ApplyComponentCSSTmplLists(&b, ".x", src, nil, lists); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		".x:has(#r0:checked) .panel[data-index=\"0\"] {\n  display: block;",
		".x:has(#r1:checked) .panel[data-index=\"1\"] {\n  display: block;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
}

func TestComponentCSSEachNested(t *testing.T) {
	const src = "@each tab in tabs\n@if flag\n&[data-i=\"{{tab.index}}\"] {\n  color: red;\n}\n@endif\n@media (max-width: 767px) {\n  &[data-i=\"{{tab.index}}\"] {\n    color: blue;\n  }\n}\n@endfor\n& {\n  display: block;\n}\n"
	lists := map[string][]map[string]string{"tabs": {{"index": "0"}, {"index": "1"}}}
	var b CSSBuckets
	if err := ApplyComponentCSSTmplLists(&b, ".x", src, map[string]string{"flag": "1"}, lists); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		".x[data-i=\"0\"] {\n  color: red;",
		".x[data-i=\"1\"] {\n  color: red;",
		"@media (max-width: 767px) {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	if !strings.Contains(out, ".x {\n  display: block;") {
		t.Errorf("循环后的规则丢失，@each 块可能被提前截断:\n%s", out)
	}
}

func TestComponentCSSEachEmptyListStillConsumesVars(t *testing.T) {
	const src = "@each s in slides\n&[data-i=\"{{s.nth}}\"] {\n  opacity: {{hold}};\n}\n@endfor\n& {\n  display: block;\n}\n"
	var b CSSBuckets
	lists := map[string][]map[string]string{"slides": {}}
	vars := map[string]string{"hold": ".5"}
	if err := ApplyComponentCSSTmplLists(&b, ".x", src, vars, lists); err != nil {
		t.Fatalf("空列表不该让变量校验误报: %v", err)
	}
	if strings.Contains(b.String(), "data-i") {
		t.Errorf("空列表不该产出循环体内的规则:\n%s", b.String())
	}

	if !strings.Contains(b.String(), ".x {\n  display: block;") {
		t.Errorf("循环后的规则丢失:\n%s", b.String())
	}
}

func TestComponentCSSEachErrors(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		lists map[string][]map[string]string
	}{
		{"列表未提供", "@each tab in missing\n& { color: red }\n@endfor\n", nil},
		{"写法缺 in", "@each tab\n& { color: red }\n@endfor\n", map[string][]map[string]string{"tab": {{"a": "1"}}}},
		{"字段未提供", "@each tab in tabs\n& { color: {{tab.nope}} }\n@endfor\n", map[string][]map[string]string{"tabs": {{"a": "1"}}}},
		{"列表提供了却没用到", "& { color: red }\n", map[string][]map[string]string{"tabs": {{"a": "1"}}}},
		{"孤立的 endfor", "& { color: red }\n@endfor\n", nil},
		{"块未闭合", "@each tab in tabs\n& { color: red }\n", map[string][]map[string]string{"tabs": {{"a": "1"}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b CSSBuckets
			if err := ApplyComponentCSSTmplLists(&b, ".x", c.src, nil, c.lists); err == nil {
				t.Errorf("应当报错却通过了：%s\n产物:\n%s", c.src, b.String())
			}
		})
	}
}

func TestComponentCSSProperty(t *testing.T) {
	const src = "@property --sky-count {\n  syntax: \"<integer>\"\n  initial-value: 0\n  inherits: false\n}\n"
	var b CSSBuckets
	if err := ApplyComponentCSS(&b, ".x", src); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	var want CSSBuckets
	want.AddPropertyDecls("--sky-count", []string{
		"syntax: \"<integer>\"",
		"initial-value: 0",
		"inherits: false",
	})
	if b.TopLevelCSS() != want.TopLevelCSS() {
		t.Errorf("顶层注册块不一致\n样式源:\n%s\nGo 调用:\n%s", b.TopLevelCSS(), want.TopLevelCSS())
	}
	if b.String() != want.String() {
		t.Errorf("@property 不该混进基础样式:\n%s", b.String())
	}
}

func TestComponentCSSKeyframesNameVar(t *testing.T) {
	const src = "@keyframes sky-marquee-{{id}} {\n  from { transform: translateX(0) }\n  to { transform: translateX(-100%) }\n}\n"
	var b CSSBuckets
	if err := ApplyComponentCSSTmpl(&b, ".x", src, map[string]string{"id": "abc"}); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "@keyframes sky-marquee-abc {") {
		t.Errorf("关键帧名里的变量未展开:\n%s", out)
	}
	if strings.Contains(out, "{{") {
		t.Errorf("产物里残留占位:\n%s", out)
	}
}

func TestComponentCSSRuleLevelIfConsumesVars(t *testing.T) {
	const src = "@if flag\n& { width: {{w}}; }\n@endif\n"
	var off CSSBuckets
	if err := ApplyComponentCSSTmpl(&off, ".x", src, map[string]string{"flag": "", "w": "10px"}); err != nil {
		t.Fatalf("未命中的规则级分支不该让变量校验误报: %v", err)
	}
	if strings.Contains(off.String(), "width") {
		t.Errorf("未命中的分支不该产出声明:\n%s", off.String())
	}

	var on CSSBuckets
	if err := ApplyComponentCSSTmpl(&on, ".x", src, map[string]string{"flag": "1", "w": "10px"}); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !strings.Contains(on.String(), "width: 10px") {
		t.Errorf("命中的分支应产出声明:\n%s", on.String())
	}
}

func TestComponentCSSBucketQueries(t *testing.T) {
	const src = "@hovernone & { box-shadow: none }\n" +
		"@container (width >= 480px) & { flex-direction: row }\n" +
		"@container (width < 400px) & .grid { grid-template-columns: 1fr }\n" +
		"@theme sky-theme --sky-density compact & .body { padding: 8px }\n" +
		"@style sky-theme --sky-card-layout horizontal & { display: grid }\n"
	var b CSSBuckets
	if err := ApplyComponentCSS(&b, ".x", src); err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	var want CSSBuckets
	want.AddHoverNone(".x", []string{"box-shadow: none"})
	want.AddContainer("(width >= 480px)", ".x", []string{"flex-direction: row"})
	want.AddContainer("(width < 400px)", ".x .grid", []string{"grid-template-columns: 1fr"})
	want.AddThemeQuery("sky-theme", "--sky-density", "compact", ".x .body", []string{"padding: 8px"})
	want.AddStyleQuery("sky-theme", "--sky-card-layout", "horizontal", ".x", []string{"display: grid"})

	if got := b.String(); got != want.String() {
		t.Errorf("基础样式产物不一致\n样式源:\n%s\nGo 调用:\n%s", got, want.String())
	}
	if got := b.ContainerQueryCSS(); got != want.ContainerQueryCSS() {
		t.Errorf("容器查询产物不一致\n样式源:\n%s\nGo 调用:\n%s", got, want.ContainerQueryCSS())
	}
}

func TestComponentCSSHovernoneIsNotHover(t *testing.T) {
	var b CSSBuckets
	if err := ApplyComponentCSS(&b, ".x", "@hovernone & { transform: none }\n"); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "@media (hover: none)") {
		t.Errorf("@hovernone 应产出 (hover: none) 块\n%s", out)
	}
	if strings.Contains(out, "(hover: hover)") {
		t.Errorf("@hovernone 被当成了 @hover（触屏等价形态会消失）\n%s", out)
	}
}

func TestComponentCSSBucketQueryErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"条件缺括号", "@container width >= 480px & { color: red }"},
		{"条件括号未闭合", "@container (width >= 480px & { color: red }"},
		{"选择器缺 &", "@container (width >= 480px) .noamp { color: red }"},
		{"hovernone 缺 &", "@hovernone .noamp { color: red }"},
		{"样式查询参数不足", "@theme sky-theme --sky-density & { color: red }"},
		{"样式查询缺选择器", "@style sky-theme --sky-card-layout horizontal { color: red }"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b CSSBuckets
			if err := ApplyComponentCSS(&b, ".x", c.src); err == nil {
				t.Errorf("应当报错却通过了：%s\n产物:\n%s", c.src, b.String())
			}
		})
	}
}

func TestComponentCSSBucketQueryInsideMedia(t *testing.T) {
	const src = "@media (max-width: 767px) {\n  @container (width >= 480px) & { color: red }\n}\n"
	var b CSSBuckets
	if err := ApplyComponentCSS(&b, ".x", src); err == nil {
		t.Errorf("容器查询写在 @media 内应当报错\n产物:\n%s", b.String())
	}
}

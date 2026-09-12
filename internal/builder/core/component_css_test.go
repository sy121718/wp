package core

import (
	"strings"
	"testing"
)

// TestComponentCSSSubstitutesVars 值变量替换成 Go 侧算出的值。
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

// TestComponentCSSTmplEmptyVarDropsDecl 空变量省略**整条声明**。
//
// 这条规则承担了迁移前 Go 里所有的 if p.X != "" { decls = append(...) }：
// 属性没设 → 变量为空 → 声明不产出。写成 width: ; 会以无效声明的形式污染产物字节。
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

// TestComponentCSSIfSegments 条件段只产出命中的那一支，且与同块其余声明合并成同一条规则。
//
// 第二条断言是迁移的字节等价前提：迁移前 Go 侧是「按条件拼一个声明切片、只 Add 一次」，
// 若条件段各自生成独立规则，产物会多出若干条同选择器的规则（语义等价但字节不同）。
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

// TestComponentCSSNestedIf 条件段支持嵌套（内层真假独立判定）。
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

// TestComponentCSSVarStrictness 变量必须双向对齐，任一侧不一致都报错。
//
// 组件迁移时 Go 侧与 .css 各写一半，拼写错误不会让构建失败、只会让某个属性从产物里消失，
// 所以这里把两个方向都钉住。
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

// TestComponentCSSOrphanEndif 孤儿 @endif 与未闭合 @if 都报错。
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

// TestComponentCSSVarsInsideMedia 跨断点 / 跨桶时变量与条件段仍然生效。
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
	// 空值的 tablet 高度整条省略：产物里不应出现第二个 height 声明。
	if strings.Count(out, "height:") != 1 {
		t.Errorf("tablet 高度为空却进了产物:\n%s", out)
	}
	// 断点块内的条件段同样生效。
	if !strings.Contains(out, "color: red") {
		t.Errorf("断点块内的条件段未生效:\n%s", out)
	}
}

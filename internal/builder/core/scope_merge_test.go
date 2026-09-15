package core

// scope_merge_test.go — 同族规则合并（PERF-016）的安全边界。
//
// 合并是「产物字节优化」里少见的**可以做到严格等价**的一类：作用域类只负责限定子树，
// 多条规则的逗号并列就是 OR。但等价性依赖三条边界，缺哪条都会静默改变优先级语义：
// 声明相同、外壳相同、成员相邻。本文件逐条把边界钉住 —— 这些是最容易被后续改动
// 悄悄放宽的地方，而放宽的症状是「某个实例的样式忽然被另一个实例的规则覆盖」。

import (
	"strings"
	"testing"
)

func TestMergeScopeSiblingsNaked(t *testing.T) {
	in := []string{
		".sky-c-a {\n  color: red;\n}",
		".sky-c-b {\n  color: red;\n}",
		".sky-c-c {\n  color: red;\n}",
	}
	got := mergeScopeSiblings(in)
	if len(got) != 1 {
		t.Fatalf("同族相邻规则应合并成 1 条，got %d 条：%v", len(got), got)
	}
	want := ".sky-c-a, .sky-c-b, .sky-c-c {\n  color: red;\n}"
	if got[0] != want {
		t.Errorf("合并结果\n got: %q\nwant: %q", got[0], want)
	}
}

func TestMergeScopeSiblingsDeclarationDiffers(t *testing.T) {
	in := []string{
		".sky-c-a {\n  color: red;\n}",
		".sky-c-b {\n  color: blue;\n}",
	}
	got := mergeScopeSiblings(in)
	if len(got) != 2 {
		t.Fatalf("声明不同不能合并，got %d 条：%v", len(got), got)
	}
}

// TestMergeScopeSiblingsNotAdjacent 中间夹着别的规则时不合并。
//
// 这条是安全边界里最关键的一条：合并会把成员折叠到首个成员的位置，
// 若中间存在同特异性的冲突规则，折叠会改变「谁覆盖谁」。
func TestMergeScopeSiblingsNotAdjacent(t *testing.T) {
	in := []string{
		".sky-c-a .x {\n  color: red;\n}",
		".unrelated {\n  color: blue;\n}",
		".sky-c-b .x {\n  color: red;\n}",
	}
	got := mergeScopeSiblings(in)
	if len(got) != 3 {
		t.Fatalf("不相邻不能合并，got %d 条：%v", len(got), got)
	}
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("第 %d 条被改动：%q", i, got[i])
		}
	}
}

// TestMergeScopeSiblingsInterleaved 交错的两族规则各自成组，互不串组。
func TestMergeScopeSiblingsInterleaved(t *testing.T) {
	in := []string{
		".sky-c-a .x {\n  color: red;\n}",
		".sky-c-b .x {\n  color: red;\n}",
		".sky-c-a .y {\n  color: blue;\n}",
		".sky-c-b .y {\n  color: blue;\n}",
	}
	got := mergeScopeSiblings(in)
	if len(got) != 2 {
		t.Fatalf("应合并成 2 组，got %d 条：%v", len(got), got)
	}
	if !strings.HasPrefix(got[0], ".sky-c-a .x, .sky-c-b .x {") {
		t.Errorf("第 1 组选择器不对：%q", got[0])
	}
	if !strings.HasPrefix(got[1], ".sky-c-a .y, .sky-c-b .y {") {
		t.Errorf("第 2 组选择器不对：%q", got[1])
	}
}

// TestMergeScopeSiblingsMultipleScopesPerSelector 一个选择器里出现多个作用域类时，
// 并列仍然等价（每个成员选择器整体保留，只是 OR 起来）。
func TestMergeScopeSiblingsMultipleScopesPerSelector(t *testing.T) {
	in := []string{
		".sky-c-a .inner .sky-c-a {\n  color: red;\n}",
		".sky-c-b .inner .sky-c-b {\n  color: red;\n}",
	}
	got := mergeScopeSiblings(in)
	if len(got) != 1 {
		t.Fatalf("应合并成 1 条，got %d 条：%v", len(got), got)
	}
	want := ".sky-c-a .inner .sky-c-a, .sky-c-b .inner .sky-c-b {\n  color: red;\n}"
	if got[0] != want {
		t.Errorf("合并结果\n got: %q\nwant: %q", got[0], want)
	}
}

func TestMergeScopeSiblingsWrappedSameShell(t *testing.T) {
	rule := " {\n  .sky-c-a:hover {\n  color: red;\n\n}\n}"
	in := []string{
		"@media (hover: hover)" + rule,
		"@media (hover: hover)" + strings.Replace(rule, ".sky-c-a", ".sky-c-b", 1),
	}
	got := mergeScopeSiblings(in)
	if len(got) != 1 {
		t.Fatalf("同外壳相邻规则应合并成 1 条，got %d 条：%v", len(got), got)
	}
	if !strings.HasPrefix(got[0], "@media (hover: hover) {\n  .sky-c-a:hover, .sky-c-b:hover {") {
		t.Errorf("外壳内的选择器没有并列：%q", got[0])
	}
	if !strings.HasSuffix(got[0], "\n}") {
		t.Errorf("外壳没有闭合：%q", got[0])
	}
}

// TestMergeScopeSiblingsWrappedShellDiffers 外壳条件不同（hover vs none、不同容器条件）时不合并。
func TestMergeScopeSiblingsWrappedShellDiffers(t *testing.T) {
	body := " {\n  .sky-x {\n  color: red;\n\n}\n}"
	in := []string{
		"@media (hover: hover)" + body,
		"@media (hover: none)" + strings.Replace(body, ".sky-x", ".sky-y", 1),
	}
	got := mergeScopeSiblings(in)
	if len(got) != 2 {
		t.Fatalf("外壳不同不能合并，got %d 条：%v", len(got), got)
	}
}

// TestMergeScopeSiblingsKeyframesUntouched 顶层注册规则（@keyframes / @property）不参与合并：
// 把两条 @keyframes 并列起来是一份无效 CSS。
func TestMergeScopeSiblingsKeyframesUntouched(t *testing.T) {
	in := []string{
		"@keyframes sky-a {\n  from { opacity: 0 }\n}",
		"@keyframes sky-b {\n  from { opacity: 0 }\n}",
		"@property --sky-x {\n  syntax: '<color>';\n}",
		"@property --sky-y {\n  syntax: '<color>';\n}",
	}
	got := mergeScopeSiblings(in)
	if len(got) != 4 {
		t.Fatalf("顶层注册规则不能合并，got %d 条：%v", len(got), got)
	}
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("第 %d 条被改动：%q", i, got[i])
		}
	}
}

// TestMergeScopeSiblingsNonScopedRulesUntouched 只有「同族」才合并：
// 选择器结构完全不同的规则即使相邻、声明相同也保持原样（避免产物里出现
// 「看不出属于哪个实例」的混合规则）。
func TestMergeScopeSiblingsNonScopedRulesUntouched(t *testing.T) {
	in := []string{
		".sky-c-a .x {\n  color: red;\n}",
		".sky-c-b .y {\n  color: red;\n}",
	}
	got := mergeScopeSiblings(in)
	if len(got) != 2 {
		t.Fatalf("选择器结构不同不能合并，got %d 条：%v", len(got), got)
	}
}

func TestNormalizeNodeScope(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{".sky-c-a", ".sky-c-#"},
		{".sky-c-abc123 .x", ".sky-c-# .x"},
		{".sky-c-a.x", ".sky-c-#.x"},
		{".sky-c-a:hover", ".sky-c-#:hover"},
		{".sky-c-a .sky-c-b", ".sky-c-# .sky-c-#"},
		{".other", ".other"},
	}
	for _, c := range cases {
		if got := normalizeNodeScope(c.in); got != c.want {
			t.Errorf("normalizeNodeScope(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 后接空格与后接点号必须折叠成不同结果 —— 否则会把结构不同的规则误判成同族。
	if normalizeNodeScope(".sky-c-a .x") == normalizeNodeScope(".sky-c-a.x") {
		t.Error("折叠把「后代选择器」与「复合选择器」混为一谈")
	}
}

// TestMergeScopeSiblingsPreservesRuleMapping 合并前后「选择器 → 声明」映射不变。
//
// 这是合并正确性的直接表达：合并只允许把多条选择器并列到同一声明块，
// 不允许增删任何 (选择器, 声明) 对。
func TestMergeScopeSiblingsPreservesRuleMapping(t *testing.T) {
	in := []string{
		".sky-c-a .x {\n  color: red;\n}",
		".sky-c-b .x {\n  color: red;\n}",
		".sky-c-a .y {\n  color: blue;\n}",
		"@media (hover: hover) {\n  .sky-c-a:hover {\n  opacity: .9;\n\n}\n}",
		"@media (hover: hover) {\n  .sky-c-b:hover {\n  opacity: .9;\n\n}\n}",
		"@keyframes sky-a {\n  from { opacity: 0 }\n}",
		".unrelated {\n  margin: 0;\n}",
	}
	before := ruleMapping(in)
	after := ruleMapping(mergeScopeSiblings(in))
	if len(before) != len(after) {
		t.Fatalf("映射条目数变了：%d → %d\nbefore=%v\nafter=%v", len(before), len(after), before, after)
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("映射 %q 变了：%d → %d", k, v, after[k])
		}
	}
}

// ruleMapping 把规则切片展开成「单条选择器 + 声明」的多重集（选择器统一折叠作用域）。
func ruleMapping(rules []string) map[string]int {
	out := map[string]int{}
	for _, raw := range rules {
		p, ok := splitRuleParts(raw)
		if !ok {
			continue
		}
		body := p.head + "\x00" + p.body
		for _, sel := range strings.Split(p.sel, ",") {
			out[normalizeNodeScope(strings.TrimSpace(sel))+"\x00"+body]++
		}
	}
	return out
}

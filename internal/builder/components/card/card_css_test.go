package card

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestCardCSSRules 迁移后关键规则逐条核对。
func TestCardCSSRules(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-c-t {\n  display: flex;",
		".sky-c-t img {",
		".sky-c-t h3 {",
		".sky-c-t p {",
		".sky-c-t a.sky-card-btn {",
		"background: var(--sky-btn-bg, var(--sky-c-primary, #2563eb))",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
	// 容器查询不进 String()：它与基础样式是两条装配路径（由装配层拼接）。
	// 混进来会让容器块落在三端媒体查询的顺序里，被后来的 hover / active 块插队。
	if strings.Contains(out, "@container") {
		t.Errorf("容器查询不该出现在基础样式里:\n%s", out)
	}
	if strings.Contains(out, "&") || strings.Contains(out, "{{") {
		t.Errorf("产物里有未替换的作用域前缀或未展开的占位:\n%s", out)
	}
}

// TestCardCSSContainerLayers 三条容器查询分别落在正确的层，且层序正确。
//
// 层序 sky-auto < sky-theme < sky-local 就是优先级：密度档位（theme）要能盖住
// 自动宽度适配（auto），作者显式声明的 --sky-card-layout（local）又要能盖住密度档位。
// 归属写错一层的表现是「主题调了没反应」或「作者调了没反应」—— 产物是一份合法 CSS，
// 浏览器不报任何错，所以只能在这里钉住。
func TestCardCSSContainerLayers(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	got := b.ContainerQueryCSS()
	for _, want := range []string{
		"@layer sky-auto {\n@container (width >= 480px) {",
		"@layer sky-theme {\n@container sky-theme style(--sky-density: compact) {",
		"@layer sky-local {\n@container sky-theme style(--sky-card-layout: horizontal) {",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("容器查询层归属不对，缺少 %q\n%s", want, got)
		}
	}
	// 字符串拼接顺序即层序，层序即优先级。
	autoIdx := strings.Index(got, "sky-auto {")
	themeIdx := strings.Index(got, "sky-theme {")
	localIdx := strings.Index(got, "sky-local {")
	if !(autoIdx < themeIdx && themeIdx < localIdx) {
		t.Errorf("层序应为 sky-auto < sky-theme < sky-local，实际下标 %d/%d/%d\n%s", autoIdx, themeIdx, localIdx, got)
	}
}

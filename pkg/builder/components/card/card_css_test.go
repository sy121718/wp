package card

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

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

	if strings.Contains(out, "@container") {
		t.Errorf("容器查询不该出现在基础样式里:\n%s", out)
	}
	if strings.Contains(out, "&") || strings.Contains(out, "{{") {
		t.Errorf("产物里有未替换的作用域前缀或未展开的占位:\n%s", out)
	}
}

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

	autoIdx := strings.Index(got, "sky-auto {")
	themeIdx := strings.Index(got, "sky-theme {")
	localIdx := strings.Index(got, "sky-local {")
	if !(autoIdx < themeIdx && themeIdx < localIdx) {
		t.Errorf("层序应为 sky-auto < sky-theme < sky-local，实际下标 %d/%d/%d\n%s", autoIdx, themeIdx, localIdx, got)
	}
}

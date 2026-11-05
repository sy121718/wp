package faq

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func TestFAQCSSRules(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-c-t {\n  display: flex;",
		"gap: var(--sky-space-4, 8px)",
		".sky-c-t details {",
		"border: 1px solid var(--sky-c-border, #e5e7eb)",
		".sky-c-t summary {",
		"list-style: none",
		"justify-content: space-between",
		".sky-c-t summary::-webkit-details-marker {",
		".sky-c-t .sky-faq-chevron {",
		"transition: transform .2s",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	if !strings.Contains(out, ".sky-c-t details[open] .sky-faq-chevron {\n  transform: rotate(180deg);") {
		t.Errorf("缺少展开态翻向:\n%s", out)
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "&") {
		t.Errorf("产物里有未展开的占位或未替换的作用域前缀:\n%s", out)
	}
}

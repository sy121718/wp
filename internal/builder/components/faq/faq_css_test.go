package faq

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestFAQCSSRules 纯静态样式：迁移后关键规则逐条核对。
func TestFAQCSSRules(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-c-t {\n  display: flex;",
		"gap: 8px",
		".sky-c-t details {",
		"border: 1px solid var(--sky-c-border, rgba(0,0,0,0.1))",
		".sky-c-t summary {",
		"list-style: none",
		"justify-content: space-between",
		".sky-c-t summary::-webkit-details-marker {",
		".sky-c-t summary::after {",
		"transition: transform .2s",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
	// 展开态由 [open] 属性选择器驱动（零 JS）——这条断掉的话展开时没有视觉反馈。
	if !strings.Contains(out, ".sky-c-t details[open] summary::after {\n  transform: rotate(45deg);") {
		t.Errorf("缺少展开态旋转:\n%s", out)
	}
	// 加减号由 content 提供（含全角加号），迁移到 CSS 文件后要保持原样。
	if !strings.Contains(out, "content: '＋'") {
		t.Errorf("缺少加减号 content:\n%s", out)
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "&") {
		t.Errorf("产物里有未展开的占位或未替换的作用域前缀:\n%s", out)
	}
}

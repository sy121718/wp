package countdown

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestCountdownCSSRules 纯静态样式：迁移后关键规则逐条核对，且产物不含未展开的占位。
func TestCountdownCSSRules(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-c-t {",
		"font-variant-numeric: tabular-nums",
		".sky-c-t .cd-item {",
		"flex-direction: column",
		".sky-c-t .cd-num {",
		"min-width: 2ch",
		".sky-c-t .cd-sep {",
		"opacity: 0.5",
		".sky-c-t .cd-label {",
		"opacity: 0.6",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "&") {
		t.Errorf("产物里有未展开的占位或未替换的作用域前缀:\n%s", out)
	}
}

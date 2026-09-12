package spacer

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestSpacerCSSRules 三端高度只在实际设置的端产出声明。
//
// 「某端为空则该端不产出」由样式源里的空值省略声明承担（迁移前是 Go 里的 if v != ""）。
func TestSpacerCSSRules(t *testing.T) {
	var b core.CSSBuckets
	CompileCSS("t", &Props{Height: Height{Desktop: "80px", Tablet: "40px", Mobile: "20px"}}, &b)
	out := b.String()
	for _, want := range []string{"height: 80px", "height: 40px", "height: 20px"} {
		if !strings.Contains(out, want) {
			t.Errorf("三端高度缺少 %q\n%s", want, out)
		}
	}

	var b2 core.CSSBuckets
	CompileCSS("t", &Props{Height: Height{Desktop: "10px"}}, &b2)
	out2 := b2.String()
	if !strings.Contains(out2, "height: 10px") {
		t.Errorf("桌面端高度缺失:\n%s", out2)
	}
	if n := strings.Count(out2, "height:"); n != 1 {
		t.Errorf("未设置的断点不应产出声明，实际 %d 条 height:\n%s", n, out2)
	}

	// 三端全空 → 一条规则都不产出（迁移前是不调用 b.Add）。
	var b3 core.CSSBuckets
	CompileCSS("t", &Props{}, &b3)
	if s := strings.TrimSpace(b3.String()); s != "" {
		t.Errorf("全空高度不应产出任何规则，实际:\n%s", s)
	}
}

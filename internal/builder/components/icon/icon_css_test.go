package icon

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func TestIconCSSRules(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{IconName: "star"}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-c-t {",
		"display: inline-flex",
		"line-height: 0",
		"width: 1.5em",
		"height: 1.5em",
		"color: currentColor",
		".sky-c-t svg {\n  width: 100%;\n  height: 100%;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	var c core.CSSBuckets
	compileCSS("t", &Props{Size: "24px", Color: "#f00"}, &c)
	out2 := c.String()
	if !strings.Contains(out2, "width: 24px") || !strings.Contains(out2, "color: #f00") {
		t.Errorf("自定义尺寸/颜色未生效:\n%s", out2)
	}
	if strings.Contains(out2, "1.5em") || strings.Contains(out2, "currentColor") {
		t.Errorf("自定义值生效时不该同时输出兜底值:\n%s", out2)
	}
}

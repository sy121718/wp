package heading

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func TestHeadingCSSSubtitleReverseSelector(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Text: "标题", Subtitle: "副题"}, &b)
	out := b.String()
	if !strings.Contains(out, ".sky-heading-sub:has(+ .sky-c-t) {") {
		t.Errorf("副标题规则缺少反向限定选择器:\n%s", out)
	}
	if strings.Contains(out, ".sky-c-t .sky-heading-sub") {
		t.Errorf("副标题用了匹配不到元素的后代选择器:\n%s", out)
	}
}

func TestHeadingCSSSegmentDelays(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Text: "标题", TextAnim: "chars"}, &b)
	out := b.String()
	if n := strings.Count(out, ":nth-child("); n != 21 {
		t.Errorf("应为 21 条分段延迟规则，实际 %d 条:\n%s", n, out)
	}
	for _, want := range []string{
		".sky-c-t .sky-h-seg:nth-child(1) {\n  animation-delay: 0ms;",
		".sky-c-t .sky-h-seg:nth-child(2) {\n  animation-delay: 40ms;",
		".sky-c-t .sky-h-seg:nth-child(20) {\n  animation-delay: 760ms;",
		".sky-c-t .sky-h-seg:nth-child(n+21) {\n  animation-delay: 800ms;",
		"animation: sky-fade-in-up 0.6s ease backwards",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	var none core.CSSBuckets
	compileCSS("t", &Props{Text: "标题"}, &none)
	if strings.Contains(none.String(), ":nth-child(") {
		t.Errorf("未开文本动画却产出了分段规则:\n%s", none.String())
	}
}

func TestHeadingCSSTextAnimDelay(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Text: "标题", TextAnim: "words", TextAnimDelay: 80}, &b)
	out := b.String()
	for _, want := range []string{"animation-delay: 0ms", "animation-delay: 80ms", "animation-delay: 1600ms"} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
}

func TestHeadingCSSHighlightSuppressesAnim(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Text: "标题", TextAnim: "chars", HighlightColor: "#ff0"}, &b)
	out := b.String()
	if strings.Contains(out, ":nth-child(") {
		t.Errorf("高亮盒与拆分动画不该叠加:\n%s", out)
	}
	if !strings.Contains(out, ".sky-c-t .sky-heading-highlight {\n  background: #ff0;") {
		t.Errorf("缺少高亮盒规则:\n%s", out)
	}
}

func TestHeadingCSSTextWrapAlways(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Text: "标题"}, &b)
	if !strings.Contains(b.String(), "text-wrap: balance") {
		t.Errorf("缺少 text-wrap: balance:\n%s", b.String())
	}
}

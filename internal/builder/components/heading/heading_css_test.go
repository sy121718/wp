package heading

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestHeadingCSSSubtitleReverseSelector 副标题规则必须用 :has() 从副标题侧反向限定。
//
// 副标题在 DOM 中是根元素的**前置兄弟**（heading.jet：subtitle 在标题标签之前），
// 后代选择器 .sky-c-x .sky-heading-sub 永远匹配不到任何元素 —— 副标题的颜色 / 字号 /
// 字重 / 间距曾因此全部静默失效。这条不变量在产物里只有一处痕迹：:has(+ <scope>)。
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

// TestHeadingCSSSegmentDelays 文本动画产出 21 条延迟规则；未开动画时一条都不产出。
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
		"animation: sky-fade-up 0.6s ease backwards",
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

// TestHeadingCSSTextAnimDelay 自定义字间延迟按倍数展开（第 21 段是 20 倍档位）。
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

// TestHeadingCSSHighlightSuppressesAnim 配了高亮盒时不做拆分动画（两者视觉上会打架）。
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

// TestHeadingCSSTextWrapAlways 平衡换行无条件产出（不支持的浏览器忽略该声明）。
func TestHeadingCSSTextWrapAlways(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Text: "标题"}, &b)
	if !strings.Contains(b.String(), "text-wrap: balance") {
		t.Errorf("缺少 text-wrap: balance:\n%s", b.String())
	}
}

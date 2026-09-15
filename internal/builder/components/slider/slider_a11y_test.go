package slider

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// slider_a11y_test.go — UI-008：轮播的键盘路径与焦点可见。
//
// 三条断言串成一条链：模板给出可聚焦容器（tabindex）→ 增强脚本给方向键路径 →
// 样式给焦点环。另有一条交叉断言钉住「圆点的 CSS 选择器与模板输出的元素一致」：
// 此前样式写成 button 选择器，而模板输出的是 <a class="sky-slider-dot">，两者对不上，
// 圆点既没有尺寸样式也没有焦点环。

// TestSliderKeyboardPath 键盘路径三件套：可聚焦容器 + 方向键处理 + 焦点环。
func TestSliderKeyboardPath(t *testing.T) {
	if !strings.Contains(sliderTemplate, `tabindex="0"`) {
		t.Errorf("容器缺少 tabindex 属性（键盘用户无法聚焦轮播，方向键没有入口）：\n%s", sliderTemplate)
	}
	for _, want := range []string{"keydown", "ArrowLeft", "ArrowRight", "Home", "End", "preventDefault"} {
		if !strings.Contains(enhanceJS, want) {
			t.Errorf("增强脚本缺少键盘处理 %q（方向键切不动轮播）", want)
		}
	}

	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-c-t:focus-visible {",
		".sky-c-t .sky-slider-arrow:focus-visible {",
		".sky-c-t .sky-slider-dot:focus-visible {",
		"box-shadow: 0 0 0 3px var(--sky-focus-ring, rgba(37,99,235,.15));",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q（焦点不可见）：\n%s", want, out)
		}
	}
}

// TestSliderDotSelectorMatchesMarkup 圆点的 CSS 选择器必须与模板输出的元素一致。
//
// 产物里的圆点是 <a class="sky-slider-dot" href="#sky-slide-…">（零 JS 也能跳转），
// 而样式原先写成 .sky-slider-dots button —— 选择器永不匹配，圆点没有尺寸与焦点环。
func TestSliderDotSelectorMatchesMarkup(t *testing.T) {
	if !strings.Contains(sliderTemplate, `class="sky-slider-dot"`) {
		t.Fatalf("模板未输出 .sky-slider-dot 圆点：\n%s", sliderTemplate)
	}
	var b core.CSSBuckets
	compileCSS("t", &Props{ShowDots: true}, &b)
	out := b.String()
	if strings.Contains(out, ".sky-slider-dots button") {
		t.Errorf("圆点样式仍写成 button 选择器（与模板的 <a> 不匹配，样式永不生效）：\n%s", out)
	}
	for _, want := range []string{".sky-c-t .sky-slider-dot {", ".sky-c-t .sky-slider-dot.is-active {"} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q：\n%s", want, out)
		}
	}
}

package slider

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

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

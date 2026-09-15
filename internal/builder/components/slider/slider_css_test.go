package slider

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestSliderCSSPerView 三端 perView 折算成 flex-basis，未设的端整段不产出。
func TestSliderCSSPerView(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{PerView: PerView{Desktop: 3, Tablet: 2, Mobile: 1}, Gap: "24px"}, &b)
	out := b.String()
	for _, want := range []string{
		"flex: 0 0 33.3333%;",
		"flex: 0 0 50.0000%;",
		"flex: 0 0 100.0000%;",
		"padding: 0 calc(24px / 2);",
		"margin: 0 calc(-24px / 2);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	// 只设桌面：两端不产出（变量为空 → 声明省略 → 规则不产出）。
	var solo core.CSSBuckets
	compileCSS("t", &Props{}, &solo)
	soloOut := solo.String()
	if !strings.Contains(soloOut, "flex: 0 0 100.0000%;") {
		t.Errorf("桌面未设 perView 时应兜底单屏一张:\n%s", soloOut)
	}
	if strings.Contains(soloOut, "@media (max-width: 1024px)") || strings.Contains(soloOut, "@media (max-width: 767px)") {
		t.Errorf("两端未设时不该产出媒体查询块:\n%s", soloOut)
	}
}

// TestSliderCSSPerViewClamp 每屏张数下限 1、上限 4（再多每屏挤不下东西）。
func TestSliderCSSPerViewClamp(t *testing.T) {
	var over core.CSSBuckets
	compileCSS("t", &Props{PerView: PerView{Desktop: 9, Tablet: 9, Mobile: 9}}, &over)
	out := over.String()
	if !strings.Contains(out, "flex: 0 0 25.0000%;") {
		t.Errorf("超出上限应收敛到 4 屏:\n%s", out)
	}
	if strings.Contains(out, "11.1111%") {
		t.Errorf("不应按 9 屏折算:\n%s", out)
	}
}

// TestSliderCSSSkeleton 骨架与增强样式都在，且没有残留占位。
func TestSliderCSSSkeleton(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{
		"scroll-snap-type: x mandatory;",
		"scroll-behavior: smooth;",
		".sky-c-t .sky-slide {",
		".sky-c-t .sky-slide > * {",
		".sky-c-t .sky-slider-arrow {",
		// 圆点在产物里是 <a class="sky-slider-dot">：选择器必须与模板一致（此前写成 button，
		// 圆点的尺寸与焦点环全部落空），见 slider_a11y_test.go 的交叉断言。
		".sky-c-t .sky-slider-dot.is-active {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "&") || strings.Contains(out, "{{") {
		t.Errorf("产物里残留占位或未替换的作用域前缀:\n%s", out)
	}
}

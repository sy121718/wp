package slider

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

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

package gallery

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func TestGalleryCSSGridColumns(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Mode: LayoutGrid}, &b)
	out := b.String()
	if !strings.Contains(out, ".sky-c-t {\n  display: grid;\n  grid-template-columns: repeat(4, 1fr);") {
		t.Errorf("桌面端应兜底 4 列:\n%s", out)
	}
	for _, bp := range []string{"@media (max-width: 1024px)", "@media (max-width: 767px)"} {
		if strings.Contains(out, bp) {
			t.Errorf("两端未设列数时不该产出 %s:\n%s", bp, out)
		}
	}

	var full core.CSSBuckets
	compileCSS("t", &Props{Mode: LayoutGrid, Grid: Grid{Columns: Columns{Desktop: 3, Tablet: 2, Mobile: 1}}}, &full)
	fullOut := full.String()
	for _, want := range []string{
		"grid-template-columns: repeat(3, 1fr);",
		"@media (max-width: 1024px) {",
		"grid-template-columns: repeat(2, 1fr);",
		"@media (max-width: 767px) {",
		"grid-template-columns: repeat(1, 1fr);",
	} {
		if !strings.Contains(fullOut, want) {
			t.Errorf("产物缺少 %q\n%s", want, fullOut)
		}
	}
}

func TestGalleryCSSCarouselSlides(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Mode: LayoutCarousel, Carousel: Carousel{SlidesPerView: SlidesPerView{Desktop: 3, Tablet: 2, Mobile: 1}}}, &b)
	out := b.String()
	for _, want := range []string{
		"flex: 0 0 33.333%;",
		"flex: 0 0 50.000%;",
		"flex: 0 0 100.000%;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	var solo core.CSSBuckets
	compileCSS("t", &Props{Mode: LayoutCarousel}, &solo)
	soloOut := solo.String()
	if !strings.Contains(soloOut, "flex: 0 0 100.000%;") {
		t.Errorf("未设 slidesPerView 时桌面应兜底单屏一张:\n%s", soloOut)
	}
	if strings.Contains(soloOut, "@media (max-width: 767px)") {
		t.Errorf("未设手机端张数时不该产出手机档:\n%s", soloOut)
	}
}

func TestGalleryCSSHoverBranches(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Hover: Hover{Overlay: "dark"}}, &b)
	out := b.String()
	if !strings.Contains(out, "transition: transform 300ms ease, box-shadow 300ms ease, filter 300ms ease;") {
		t.Errorf("开遮罩后应有过渡声明（时长取默认 300ms）:\n%s", out)
	}
	if !strings.Contains(out, ".sky-c-t .gi::after {") || !strings.Contains(out, "background: rgba(0,0,0,0.35);") {
		t.Errorf("深色遮罩的 ::after 覆盖层缺失:\n%s", out)
	}
	if strings.Contains(out, ".sky-c-t .gi:hover {\n") {
		t.Errorf("未开缩放与阴影加深时不该产出 .gi:hover 规则:\n%s", out)
	}

	var scaleOnly core.CSSBuckets
	compileCSS("t", &Props{Hover: Hover{Scale: "1.05", Duration: "200ms"}}, &scaleOnly)
	scaleOut := scaleOnly.String()
	if !strings.Contains(scaleOut, ".sky-c-t .gi:hover {\n  transform: scale(1.05);") {
		t.Errorf("缩放规则缺失:\n%s", scaleOut)
	}
	if strings.Contains(scaleOut, "::after") {
		t.Errorf("未开遮罩时不该产出覆盖层:\n%s", scaleOut)
	}
}

func TestGalleryCSSContainerQuery(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Mode: LayoutGrid}, &b)
	if strings.Contains(b.String(), "@container") {
		t.Errorf("容器查询不该混进基础样式:\n%s", b.String())
	}
	want := "@layer sky-auto {\n@container (width < 400px) {\n  .sky-c-t {"
	if !strings.Contains(b.ContainerQueryCSS(), want) {
		t.Errorf("容器查询应落在 sky-auto 层，缺少 %q\n%s", want, b.ContainerQueryCSS())
	}
}

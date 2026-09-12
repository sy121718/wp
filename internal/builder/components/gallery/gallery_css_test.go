package gallery

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestGalleryCSSGridColumns 三端列数：桌面兜底 4，两端未设时**整段不产出**。
//
// 这条是迁移时最容易静默走样的地方：样式源里 display: grid 与 grid-template-columns
// 写在同一条规则里，若只让列数那一条随变量省略，媒体查询块会只剩一个 display: grid ——
// 产物依旧是合法 CSS，只是窄屏上多了一条无意义规则。
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

// TestGalleryCSSCarouselSlides 单屏宽度按 slidesPerView 三端换算，且缺省端不产出。
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

	// 只设桌面：桌面兜底为「单屏一张」，两端不产出。
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

// TestGalleryCSSHoverBranches 悬浮三态各自独立：只开遮罩时不该产出 .gi:hover 缩放规则。
//
// 迁移前由 len(hoverDecls) > 0 守卫；迁到样式源后这个守卫变成了「两条声明都随变量省略、
// 整块声明列表为空即不产出规则」—— 语义相同但依赖解析器的空声明早退，值得钉住。
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

	// 只开缩放：产出 hover 规则但不产出遮罩层。
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

// TestGalleryCSSContainerQuery 窄容器降为单列走容器查询，不与三端媒体查询混在同一处。
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

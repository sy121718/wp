package loader

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func loaderCSSFor(p *Props) string {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

// TestLoaderCSSVariantsIsolated 每个变体只产出自己的规则与关键帧。
func TestLoaderCSSVariantsIsolated(t *testing.T) {
	cases := []struct {
		variant string
		rule    string
		frames  string
	}{
		{VariantSpinner, ".sky-loader-ring {", "@keyframes sky-loader-spin {"},
		{VariantDots, ".sky-loader-dot {", "@keyframes sky-loader-dot {"},
		{VariantBars, ".sky-loader-bar {", "@keyframes sky-loader-bar {"},
		{VariantPulse, ".sky-loader-pulse {", "@keyframes sky-loader-pulse {"},
		{VariantPlane, ".sky-loader-plane {", "@keyframes sky-loader-plane {"},
		{VariantGrid, ".sky-loader-grid {", "@keyframes sky-loader-grid {"},
		{VariantOrbit, ".sky-loader-orbit {", "@keyframes sky-loader-orbit {"},
		{VariantWave, ".sky-loader-wave {", "@keyframes sky-loader-wave {"},
		{VariantBounce, ".sky-loader-bounce {", "@keyframes sky-loader-bounce {"},
	}
	for _, c := range cases {
		out := loaderCSSFor(&Props{Variant: c.variant})
		if !strings.Contains(out, c.rule) || !strings.Contains(out, c.frames) {
			t.Errorf("变体 %s 的规则或关键帧缺失:\n%s", c.variant, out)
		}
		for _, other := range cases {
			if other.variant != c.variant && strings.Contains(out, other.frames) {
				t.Errorf("变体 %s 混进了 %s 的关键帧（形态互斥被破坏）", c.variant, other.variant)
			}
		}
	}
}

// TestLoaderCSSPhaseDelays 相位延迟按序号逐条展开，条数与序号都必须对。
//
// 这些规则由 @each 展开：少一条就是「某个点不掉队」—— 九宫格与波浪条全靠延迟错开，
// 漏掉相位会让整个动画看起来是整体闪动而非流动。
func TestLoaderCSSPhaseDelays(t *testing.T) {
	cases := []struct {
		variant string
		sel     string
		want    []string
	}{
		{VariantDots, ".sky-loader-dot", []string{":nth-child(2) {\n  animation-delay: .15s;", ":nth-child(3) {\n  animation-delay: .3s;"}},
		{VariantBars, ".sky-loader-bar", []string{":nth-child(4) {\n  animation-delay: .45s;"}},
		{VariantWave, ".sky-loader-wave i", []string{":nth-child(1) {\n  animation-delay: -1.2s;", ":nth-child(5) {\n  animation-delay: -.8s;"}},
		{VariantBounce, ".sky-loader-bounce", []string{":nth-child(3) {\n  animation-delay: 0s;"}},
	}
	for _, c := range cases {
		out := loaderCSSFor(&Props{Variant: c.variant})
		for _, want := range c.want {
			full := ".sky-c-t " + c.sel + want
			if !strings.Contains(out, full) {
				t.Errorf("变体 %s 缺少相位 %q\n%s", c.variant, full, out)
			}
		}
	}
	// 九宫格 9 条、波浪 5 条 —— 条数直接决定动画形态。
	grid := loaderCSSFor(&Props{Variant: VariantGrid})
	if strings.Count(grid, ".sky-loader-grid i:nth-child(") != 9 {
		t.Errorf("九宫格应有 9 条相位延迟:\n%s", grid)
	}
	wave := loaderCSSFor(&Props{Variant: VariantWave})
	if strings.Count(wave, ".sky-loader-wave i:nth-child(") != 5 {
		t.Errorf("波浪条应有 5 条相位延迟:\n%s", wave)
	}
}

// TestLoaderCSSNoInvalidDecl 颜色为空时不产出无效声明。
//
// 迁移前 Go 侧用的是 core.CSSDecl("color", p.Color)，空值只跳过参数、
// 仍留下一条 `color: ` —— 浏览器会丢弃它，但产物字节里带着一条无效声明。
// 样式源的语义是「变量为空即整条省略」，顺手把这个既有缺陷去掉了。
func TestLoaderCSSNoInvalidDecl(t *testing.T) {
	out := loaderCSSFor(&Props{})
	if strings.Contains(out, "color: ;") || strings.Contains(out, ": ;") || strings.Contains(out, ":  ;") {
		t.Errorf("产物里有空值声明:\n%s", out)
	}
	if !strings.Contains(out, "--sky-loader-size: 32px;") {
		t.Errorf("尺寸未兜底 32px:\n%s", out)
	}
	if !strings.Contains(loaderCSSFor(&Props{Size: "48px", Color: "#f00"}), "color: #f00;") {
		t.Errorf("自定义颜色缺失")
	}
}

// TestLoaderCSSLabel 文案是可选规则。
func TestLoaderCSSLabel(t *testing.T) {
	if strings.Contains(loaderCSSFor(&Props{}), ".sky-loader-label") {
		t.Errorf("无文案时不该产出标签规则")
	}
	if !strings.Contains(loaderCSSFor(&Props{Label: "加载中"}), ".sky-c-t .sky-loader-label {") {
		t.Errorf("有文案时应产出标签规则")
	}
}

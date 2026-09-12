// Package loader 实现 core.loader 加载器组件（分类效果库「加载 loader」分类落地：
// 多形态纯 CSS 加载指示，零客户端 JS、确定性产物）。
package loader

import (
	_ "embed" // loader.css 经 //go:embed 打进二进制
	"fmt"
	"strconv"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.loader"

// 形态取值。
const (
	VariantSpinner = "spinner" // 环形旋转（经典）
	VariantDots    = "dots"    // 三点跳动
	VariantBars    = "bars"    // 条形伸缩
	VariantPulse   = "pulse"   // 双圈脉冲
	VariantPlane   = "plane"   // 平面翻转（3D）
	VariantGrid    = "grid"    // 九宫格脉冲
	VariantOrbit   = "orbit"   // 环绕点
	VariantWave    = "wave"    // 波浪条（SpinKit wave：五条相位错开）
	VariantBounce  = "bounce"  // 三点弹跳（SpinKit three-bounce）
)

// Props loader 属性。
type Props struct {
	// Variant 形态：spinner / dots / bars / pulse / plane / grid / orbit / wave / bounce（默认 spinner）。
	Variant string `json:"variant,omitempty" ct:"select,spinner=环形,dots=点阵,bars=条形,pulse=脉冲,plane=平面翻转,grid=九宫格,orbit=环绕,wave=波浪,bounce=弹跳,default=spinner,sec=content,label=形态"`
	// Size 主视觉尺寸（环形/脉冲直径；点阵/条形按比例缩放。默认 32px）。
	Size string `json:"size,omitempty" ct:"dimension,maxlen=20,sec=style,label=尺寸"`
	// Color 颜色（空 = currentColor 跟随文字色）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=颜色"`
	// Label 无障碍与可见文本（如「加载中…」；空 = 仅 aria-label）。
	Label string `json:"label,omitempty" ct:"text,maxlen=100,sec=content,label=文本"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
		Translatable:  []string{"label"},
	},
}

// effectiveVariant 空值缺省 spinner。
func effectiveVariant(p *Props) string {
	if p.Variant == "" {
		return VariantSpinner
	}
	return p.Variant
}

// validateExtra 校验：形态白名单 + CSS 安全值。
func validateExtra(p *Props, nodeID string) (err error) {
	if _, ok := shapeByVariant[effectiveVariant(p)]; !ok {
		return fmt.Errorf("无效的加载器形态: %q", p.Variant)
	}
	if p.Size != "" && !core.IsSafeCSSValue(p.Size) {
		return fmt.Errorf("无效的尺寸: %q", p.Size)
	}
	if p.Color != "" && !core.IsSafeCSSValue(p.Color) {
		return fmt.Errorf("无效的颜色: %q", p.Color)
	}
	return nil
}

// compileCSS 容器/形态/尺寸/颜色样式（keyframes 组件内私有：加载节奏
// loaderCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组。
//
//go:embed loader.css
var loaderCSS string

// compileCSS 加载指示器样式（9 种形态）。
//
// Go 侧只做两件事：补尺寸默认值、把「用哪个形态」翻成布尔量；
// 各形态的相位延迟（按序号逐条）交给样式源的 @each 展开。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	size := p.Size
	if size == "" {
		size = "32px"
	}
	variant := effectiveVariant(p)

	// 相位延迟列表：nth 是子元素序号（从 1 起），delay 直接进 animation-delay。
	delays := func(vals ...string) []map[string]string {
		out := make([]map[string]string, 0, len(vals))
		for i, d := range vals {
			out = append(out, map[string]string{"nth": strconv.Itoa(i + 1), "delay": d})
		}
		return out
	}
	// dots / bars 的首个子元素不设延迟，序号从 2 起。
	shifted := func(vals ...string) []map[string]string {
		out := make([]map[string]string, 0, len(vals))
		for i, d := range vals {
			out = append(out, map[string]string{"nth": strconv.Itoa(i + 2), "delay": d})
		}
		return out
	}
	// 九宫格的延迟是等差序列，按对角线错落，用一位小数（与迁移前同一格式）。
	gridDelays := make([]map[string]string, 0, 9)
	for i, d := range []float64{0, .1, .2, .1, .2, .3, .2, .3, .4} {
		gridDelays = append(gridDelays, map[string]string{
			"nth":   strconv.Itoa(i + 1),
			"delay": fmt.Sprintf("%.1fs", d),
		})
	}

	vars := map[string]string{
		"size":     size,
		"color":    p.Color,
		"spinner":  core.BoolVar(variant == VariantSpinner),
		"dots":     core.BoolVar(variant == VariantDots),
		"bars":     core.BoolVar(variant == VariantBars),
		"pulse":    core.BoolVar(variant == VariantPulse),
		"plane":    core.BoolVar(variant == VariantPlane),
		"grid":     core.BoolVar(variant == VariantGrid),
		"orbit":    core.BoolVar(variant == VariantOrbit),
		"wave":     core.BoolVar(variant == VariantWave),
		"bounce":   core.BoolVar(variant == VariantBounce),
		"hasLabel": core.BoolVar(p.Label != ""),
	}
	lists := map[string][]map[string]string{
		"dotsDelays":   shifted(".15s", ".3s"),
		"barsDelays":   shifted(".15s", ".3s", ".45s"),
		"gridDelays":   gridDelays,
		"waveDelays":   delays("-1.2s", "-1.1s", "-1s", "-.9s", "-.8s"),
		"bounceDelays": delays("-.32s", "-.16s", "0s"),
	}
	if err := core.ApplyComponentCSSTmplLists(b, sel, loaderCSS, vars, lists); err != nil {
		panic(fmt.Sprintf("loader 组件样式解析失败: %v", err))
	}
}

// init 注册加载器组件。
func init() {
	core.Register(Widget)
}

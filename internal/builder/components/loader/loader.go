// Package loader 实现 core.loader 加载器组件（分类效果库「加载 loader」分类落地：
// 多形态纯 CSS 加载指示，零客户端 JS、确定性产物）。
package loader

import (
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
// 与通用循环动效的 2.4s 节奏不同，仅本组件消费）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	size := p.Size
	if size == "" {
		size = "32px"
	}
	b.Add(core.BreakpointDesktop, sel, []string{
		"display: inline-flex",
		"align-items: center",
		"gap: 12px",
		core.CSSDecl("color", p.Color),
		"--wp-loader-size: " + size,
	})
	switch effectiveVariant(p) {
	case VariantSpinner:
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-ring", []string{
			"width: var(--wp-loader-size)",
			"height: var(--wp-loader-size)",
			"border: 3px solid rgba(0,0,0,.12)",
			"border-top-color: currentColor",
			"border-radius: 50%",
			"animation: wp-loader-spin .8s linear infinite",
		})
		b.AddKeyframesDecls("wp-loader-spin", []string{
			"to { transform: rotate(360deg) }",
		})
	case VariantDots:
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-dot", []string{
			"width: calc(var(--wp-loader-size) / 4)",
			"height: calc(var(--wp-loader-size) / 4)",
			"border-radius: 50%",
			"background: currentColor",
			"animation: wp-loader-dot .6s ease-in-out infinite alternate",
		})
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-dot:nth-child(2)", []string{"animation-delay: .15s"})
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-dot:nth-child(3)", []string{"animation-delay: .3s"})
		b.AddKeyframesDecls("wp-loader-dot", []string{
			"from { transform: translateY(0); opacity: .4 }",
			"to { transform: translateY(calc(var(--wp-loader-size) / -4)); opacity: 1 }",
		})
	case VariantBars:
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-bar", []string{
			"width: calc(var(--wp-loader-size) / 8)",
			"height: calc(var(--wp-loader-size) * .75)",
			"border-radius: 2px",
			"background: currentColor",
			"animation: wp-loader-bar .9s ease-in-out infinite",
		})
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-bar:nth-child(2)", []string{"animation-delay: .15s"})
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-bar:nth-child(3)", []string{"animation-delay: .3s"})
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-bar:nth-child(4)", []string{"animation-delay: .45s"})
		b.AddKeyframesDecls("wp-loader-bar", []string{
			"0%, 100% { transform: scaleY(.4) }",
			"50% { transform: scaleY(1) }",
		})
	case VariantPulse:
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-pulse", []string{
			"position: relative",
			"width: var(--wp-loader-size)",
			"height: var(--wp-loader-size)",
		})
		pulseBase := []string{
			"content: ''",
			"position: absolute",
			"inset: 0",
			"border-radius: 50%",
			"border: 3px solid currentColor",
		}
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-pulse::before", append(pulseBase, "animation: wp-loader-pulse 1.2s ease-out infinite"))
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-pulse::after", append(pulseBase, "animation: wp-loader-pulse 1.2s ease-out .6s infinite"))
		b.AddKeyframesDecls("wp-loader-pulse", []string{
			"from { transform: scale(.5); opacity: 1 }",
			"to { transform: scale(1.4); opacity: 0 }",
		})
	case VariantPlane:
		// 平面翻转：单方块绕 X/Y 轴循环翻转（3D，合成器友好）。
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-plane", []string{
			"width: var(--wp-loader-size)",
			"height: var(--wp-loader-size)",
			"background: currentColor",
			"border-radius: 4px",
			"animation: wp-loader-plane 1.6s ease-in-out infinite",
		})
		b.AddKeyframesDecls("wp-loader-plane", []string{
			"0%, 100% { transform: perspective(400px) rotateX(0) rotateY(0) }",
			"25% { transform: perspective(400px) rotateX(180deg) rotateY(0) }",
			"50% { transform: perspective(400px) rotateX(180deg) rotateY(180deg) }",
			"75% { transform: perspective(400px) rotateX(0) rotateY(180deg) }",
		})
	case VariantGrid:
		// 九宫格脉冲：3×3 方块按对角线错落缩放。
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-grid", []string{
			"display: grid",
			"grid-template-columns: repeat(3, 1fr)",
			"gap: calc(var(--wp-loader-size) / 12)",
			"width: var(--wp-loader-size)",
			"height: var(--wp-loader-size)",
		})
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-grid i", []string{
			"background: currentColor",
			"border-radius: 2px",
			"animation: wp-loader-grid 1.3s ease-in-out infinite",
		})
		delays := []float64{0, .1, .2, .1, .2, .3, .2, .3, .4}
		for i, d := range delays {
			b.Add(core.BreakpointDesktop, sel+" .wp-loader-grid i:nth-child("+strconv.Itoa(i+1)+")",
				[]string{fmt.Sprintf("animation-delay: %.1fs", d)})
		}
		b.AddKeyframesDecls("wp-loader-grid", []string{
			"0%, 70%, 100% { transform: scale(1); opacity: 1 }",
			"35% { transform: scale(.45); opacity: .35 }",
		})
	case VariantOrbit:
		// 环绕点：单点绕容器中心匀速旋转（线性，最省合成开销）。
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-orbit", []string{
			"position: relative",
			"width: var(--wp-loader-size)",
			"height: var(--wp-loader-size)",
			"animation: wp-loader-orbit 1.4s linear infinite",
		})
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-orbit::before", []string{
			"content: ''",
			"position: absolute",
			"top: 0",
			"left: 50%",
			"width: calc(var(--wp-loader-size) / 5)",
			"height: calc(var(--wp-loader-size) / 5)",
			"margin-left: calc(var(--wp-loader-size) / -10)",
			"border-radius: 50%",
			"background: currentColor",
		})
		b.AddKeyframesDecls("wp-loader-orbit", []string{"to { transform: rotate(360deg) }"})
	case VariantWave:
		// 波浪条（SpinKit wave）：五根竖条共用一条 scaleY 帧，靠负延迟错开相位。
		// 负延迟让首帧就落在动画中段——加载态刚出现时不会先静止一拍。
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-wave", []string{
			"display: flex",
			"align-items: center",
			"gap: calc(var(--wp-loader-size) / 8)",
			"height: var(--wp-loader-size)",
		})
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-wave i", []string{
			"width: calc(var(--wp-loader-size) / 10)",
			"height: 100%",
			"border-radius: 1px",
			"background: currentColor",
			"animation: wp-loader-wave 1.2s ease-in-out infinite",
		})
		for i, d := range []string{"-1.2s", "-1.1s", "-1s", "-.9s", "-.8s"} {
			b.Add(core.BreakpointDesktop,
				fmt.Sprintf("%s .wp-loader-wave i:nth-child(%d)", sel, i+1),
				[]string{"animation-delay: " + d})
		}
		b.AddKeyframesDecls("wp-loader-wave", []string{
			"0%, 40%, 100% { transform: scaleY(.4) }",
			"20% { transform: scaleY(1) }",
		})
	case VariantBounce:
		// 三点弹跳（SpinKit three-bounce）：缩放出现 + 相位错开，同样用负延迟。
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-bounce", []string{
			"width: calc(var(--wp-loader-size) / 4)",
			"height: calc(var(--wp-loader-size) / 4)",
			"border-radius: 50%",
			"background: currentColor",
			"animation: wp-loader-bounce 1.4s ease-in-out infinite both",
		})
		for i, d := range []string{"-.32s", "-.16s", "0s"} {
			b.Add(core.BreakpointDesktop,
				fmt.Sprintf("%s .wp-loader-bounce:nth-child(%d)", sel, i+1),
				[]string{"animation-delay: " + d})
		}
		b.AddKeyframesDecls("wp-loader-bounce", []string{
			"0%, 80%, 100% { transform: scale(0) }",
			"40% { transform: scale(1) }",
		})
	}
	if p.Label != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-loader-label", []string{
			"font-size: 14px",
			"line-height: 1",
		})
	}
}

// init 注册加载器组件。
func init() {
	core.Register(Widget)
}

// Package badge 实现 core.badge 徽章组件（对标 GrapesJS badge 组件生态）。
//
// 轻量装饰性原子组件：单行文字 + 三种外观范式（solid 实心 / outline 描边 / soft 浅底），
// 支持颜色覆盖（色值或主题 Token），零客户端 JS。缺省主色走主题 Token
// var(--wp-c-primary) 并带兜底，与容器/按钮等组件保持同一取色约定。
package badge

import (
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.badge"

// 变体（外观范式）。
const (
	VariantSolid   = "solid"   // 实心：主色背景 + 白字
	VariantOutline = "outline" // 描边：主色边框 + 主色文字
	VariantSoft    = "soft"    // 浅底：主色浅底 + 主色文字
)

// defaultColor 缺省主色（主题 Token + 兜底，与 tabs/infobox 的取色约定对齐）。
const defaultColor = "var(--wp-c-primary, #2563eb)"

// Props badge 属性。
type Props struct {
	// Text 徽章文字。
	Text string `json:"text,omitempty" ct:"text,maxlen=50,sec=content,label=文字"`
	// Variant 风格：solid / outline / soft（默认 solid）。
	Variant string `json:"variant,omitempty" ct:"select,solid=实心,outline=描边,soft=浅底,default=solid,sec=style,label=风格"`
	// Color 主色（色值或主题 Token）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=颜色"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
	},
}

// validateExtra 关系性校验：徽章必须有文字。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.Text == "" {
		return fmt.Errorf("徽章必须提供文字")
	}
	return nil
}

// effectiveColor 有效主色（空则主题 Token 兜底）。
func effectiveColor(p *Props) string {
	if p.Color == "" {
		return defaultColor
	}
	return p.Color
}

// compileCSS 徽章基础盒模型 + 三种变体样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	color := effectiveColor(p)
	variant := p.Variant
	if variant == "" {
		variant = VariantSolid
	}

	base := []string{
		"display: inline-flex",
		"align-items: center",
		"justify-content: center",
		"padding: 2px 10px",
		"border-radius: 9999px",
		"font-size: 0.75rem",
		"font-weight: 600",
		"line-height: 1.6",
		"white-space: nowrap",
	}
	switch variant {
	case VariantOutline:
		base = append(base,
			core.CSSDecl("color", color),
			"background: transparent",
			core.CSSDecl("border", "1px", "solid", color),
		)
	case VariantSoft:
		// 浅底：主色 12% 混合透明，文字用主色。
		base = append(base,
			core.CSSDecl("color", color),
			core.CSSDecl("background", "color-mix(in srgb, "+color+" 12%, transparent)"),
		)
	default: // solid
		base = append(base,
			core.CSSDecl("background", color),
			"color: #fff",
		)
	}
	b.Add(core.BreakpointDesktop, sel, base)
}

// init 注册徽章组件。
func init() {
	core.Register(Widget)
}

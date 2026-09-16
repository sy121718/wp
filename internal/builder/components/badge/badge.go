// Package badge 实现 core.badge 徽章组件（对标 GrapesJS badge 组件生态）。
//
// 轻量装饰性原子组件：单行文字 + 三种外观范式（solid 实心 / outline 描边 / soft 浅底），
// 支持颜色覆盖（色值或主题 Token），零客户端 JS。缺省主色走主题 Token
// var(--sky-c-primary) 并带兜底，与容器/按钮等组件保持同一取色约定。
package badge

import (
	_ "embed" // badge.css 经 //go:embed 打进二进制
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
const defaultColor = "var(--sky-c-primary, #2563eb)"

// Props badge 属性。
type Props struct {
	// Text 徽章文字。
	Text string `json:"text,omitempty" ct:"text,maxlen=50,sec=content,label=文字"`
	// Variant 风格：solid / outline / soft（默认 solid）。
	Variant string `json:"variant,omitempty" ct:"select,solid=实心,outline=描边,soft=浅底,default=solid,sec=style,label=风格"`
	// Color 主色（色值或主题 Token）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=颜色"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "徽章",
		Hint:            "文本徽章",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"text":    "新品",
			"variant": "solid",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,
		// Translatable 可翻译字段白名单（多语言 P5b，docs/06-D §7.5 决策 F6）：
		// 只有这里列出的字段参与内容翻译，未声明字段永不翻译。
		Translatable: []string{"text"},
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

// badgeCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed badge.css
var badgeCSS string

// compileCSS 徽章基础盒模型 + 三种变体样式。
//
// 三种变体各自是一组**不同的声明**（不是同一个属性的不同取值），所以在样式源里用
// @if 条件段表达；主色由 Props 算出，作为值变量传入。空变体与未知变体一律兜底为实心，
// 与迁移前 switch 的 default 分支行为一致。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	variant := p.Variant
	switch variant {
	case VariantOutline, VariantSoft:
	default:
		variant = VariantSolid
	}
	vars := map[string]string{
		"solid":   "",
		"outline": "",
		"soft":    "",
		"color":   effectiveColor(p),
	}
	vars[variant] = "1"
	if err := core.ApplyComponentCSSTmpl(b, sel, badgeCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("badge 组件样式解析失败: %v", err))
	}
}

// init 注册徽章组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("badge", badgeTemplate)
}

// badgeTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed badge.jet
var badgeTemplate string

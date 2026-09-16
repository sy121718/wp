// Package icon 实现 core.icon 图标组件（对标 GrapesJS icon 组件生态）。
//
// 装饰性原子组件：内置 8 个白名单 SVG 图标（24 viewBox，stroke=currentColor），
// 支持尺寸与颜色覆盖，零客户端 JS。图标路径由 BuildView 按 IconName 从白名单
// 选取，模板输出 <svg> 包裹，杜绝任意 SVG 源码注入。
package icon

import (
	_ "embed" // icon.css 经 //go:embed 打进二进制
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.icon"

// 默认图标名（IconName 空时兜底）。
const defaultIconName = "star"

// builtinIcons 内置白名单图标（24 viewBox，stroke=currentColor，装饰性）。
// 键与 Props.IconName 的 select 选项一一对应。
var builtinIcons = map[string]string{
	"star":        `<path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" stroke-linejoin="round"/>`,
	"heart":       `<path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z" stroke-linejoin="round"/>`,
	"check":       `<path d="M20 6L9 17l-5-5" stroke-linecap="round" stroke-linejoin="round"/>`,
	"arrow-right": `<path d="M5 12h14M13 6l6 6-6 6" stroke-linecap="round" stroke-linejoin="round"/>`,
	"arrow-left":  `<path d="M19 12H5M11 6l-6 6 6 6" stroke-linecap="round" stroke-linejoin="round"/>`,
	"info":        `<circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/>`,
	"close":       `<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>`,
	"search":      `<circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>`,
}

// Props icon 属性。
type Props struct {
	// IconName 内置图标名（star/heart/check/arrow-right/arrow-left/info/close/search，默认 star）。
	IconName string `json:"iconName,omitempty" ct:"select,star=星形,heart=心形,check=对勾,arrow-right=右箭头,arrow-left=左箭头,info=信息,close=关闭,search=搜索,default=star,sec=content,label=图标样式"`
	// Size 图标尺寸（如 24px / 2em，空则默认 1.5em）。
	Size string `json:"size,omitempty" ct:"dimension,maxlen=20,sec=style,label=尺寸"`
	// Color 图标颜色（currentColor 或色值/主题 Token）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=颜色"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "图标",
		Hint:            "通用 SVG 图标",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"iconName": "star",
			"size":     "24px",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,
	},
}

// validateExtra 关系性校验：图标名必须在白名单内。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.IconName != "" {
		if _, ok := builtinIcons[p.IconName]; !ok {
			return fmt.Errorf("无效的内置图标: %q（仅 star/heart/check/arrow-right/arrow-left/info/close/search）", p.IconName)
		}
	}
	return nil
}

// iconCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed icon.css
var iconCSS string

// compileCSS 图标容器尺寸与颜色样式。
//
// 尺寸与颜色在 Go 侧兜底（缺省 1.5em / currentColor），样式源只负责属性怎么组合。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{"size": "1.5em", "color": "currentColor"}
	if p.Size != "" {
		vars["size"] = p.Size
	}
	if p.Color != "" {
		vars["color"] = p.Color
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, iconCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("icon 组件样式解析失败: %v", err))
	}
}

// init 注册图标组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("icon", iconTemplate)
}

// iconTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed icon.jet
var iconTemplate string

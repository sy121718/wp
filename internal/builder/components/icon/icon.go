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

// iconNames 组件白名单：编辑器里可选的名字 → 基座图标库（core，lucide 1868 枚）里的名字。
//
// 以前这里手写了一份 8 个图标的 path 表 —— 那是**第二份图标库**：同一个箭头
// 在本组件与基座库里的描边、圆角都不一致，改一处另一处不会跟着变。
// 现在白名单只声明「哪些图标可选」，路径一律从 core 取。
//
// 映射说明：close 在 lucide 里叫 x（同名不同字，映射在这里显式写出）。
var iconNames = map[string]string{
	"star":        "star",
	"heart":       "heart",
	"check":       "check",
	"arrow-right": "arrow-right",
	"arrow-left":  "arrow-left",
	"info":        "info",
	"close":       "x",
	"search":      "search",
}

// iconPath 取白名单图标的内部元素（来自基座图标库）。
func iconPath(name string) (string, bool) {
	lib, ok := iconNames[name]
	if !ok {
		return "", false
	}
	return core.IconInner(lib)
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
		if _, ok := iconNames[p.IconName]; !ok {
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

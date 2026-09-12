// Package list 实现 core.list：列表组件（对标 WD wd_list）。
// 支持三种样式：自定义图标 / 序号 / 圆点；每项可带链接。
package list

import (
	_ "embed" // list.css 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.list"

func init() { core.Register(&Component{}) }

// Component 列表组件（原子）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// Translatable 实现 core.TranslatableProvider：可翻译字段白名单（多语言 P5b，
// docs/06-D §7.5 决策 F6）。只有这里列出的字段参与内容翻译，未声明字段永不翻译。
func (c *Component) Translatable() []string { return []string{"text"} }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

// Item 列表项。
type Item struct {
	// Icon 自定义图标名（内置图标集：check/star/arrow-right/cross 等；空则用样式默认）。
	Icon string `json:"icon,omitempty"`
	// Text 文本内容。
	Text string `json:"text,omitempty"`
	// Link 可选项链接。
	Link string `json:"link,omitempty"`
}

// Style 列表样式：icon 自定义图标 / number 序号 / dot 圆点。
type Style string

const (
	StyleIcon   Style = "icon"
	StyleNumber Style = "number"
	StyleDot    Style = "dot"
)

// Props 列表属性。
type Props struct {
	// Items 列表项（repeater）。
	Items []Item `json:"items,omitempty"`
	// Style 列表样式：icon / number / dot。
	Style Style `json:"style,omitempty" ct:"select,icon=图标,number=序号,dot=圆点,default=icon,sec=content,label=列表样式"`
	// IconColor 图标颜色（样式为 icon 时）。
	IconColor string `json:"iconColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标颜色"`
	// IconSize 图标尺寸（px，样式为 icon 时）。
	IconSize string `json:"iconSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=图标尺寸"`
	// TextColor 文本颜色。
	TextColor string `json:"textColor,omitempty" ct:"color,maxlen=200,sec=style,label=文本颜色"`
	// TextSize 文本字号。
	TextSize string `json:"textSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=文本字号"`
	// LinkColor 链接颜色。
	LinkColor string `json:"linkColor,omitempty" ct:"color,maxlen=200,sec=style,label=链接颜色"`
	// LinkColorHover 链接悬停颜色。
	LinkColorHover string `json:"linkColorHover,omitempty" ct:"color,maxlen=200,sec=style,label=链接悬停颜色"`
	// IconBgColor 图标背景色（圆形底）。
	IconBgColor string `json:"iconBgColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标背景色"`
	// IconBgColorHover 图标悬停背景色。
	IconBgColorHover string `json:"iconBgColorHover,omitempty" ct:"color,maxlen=200,sec=style,label=图标悬停背景色"`
	// IconColorHover 图标悬停颜色。
	IconColorHover string `json:"iconColorHover,omitempty" ct:"color,maxlen=200,sec=style,label=图标悬停颜色"`
	// Align 对齐：left / center / right。
	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=left,sec=style,label=对齐"`
	// Spacing 项间距（px）。
	Spacing string `json:"spacing,omitempty" ct:"dimension,maxlen=20,sec=style,label=项间距"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Validate 校验。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 列表为原子组件，不允许子节点", node.ID)
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	switch p.Style {
	case "", StyleIcon, StyleNumber, StyleDot:
	default:
		return fmt.Errorf("节点 %s: 无效的列表样式 %q", node.ID, p.Style)
	}
	if p.Style == StyleIcon {
		for _, it := range p.Items {
			if it.Icon != "" {
				if _, ok := core.IconSVG(it.Icon); !ok {
					return fmt.Errorf("节点 %s: 未知图标 %q", node.ID, it.Icon)
				}
			}
		}
	}
	for i, it := range p.Items {
		if link := strings.TrimSpace(it.Link); link != "" && !core.IsSafeURL(link) {
			return fmt.Errorf("节点 %s: 第 %d 项链接含危险协议: %q", node.ID, i+1, it.Link)
		}
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
	if err = core.ValidateSpec(&p, node.ID); err != nil {
		return err
	}
	return nil
}

// listCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed list.css
var listCSS string

// compileCSS 列表样式。
//
// 每个「可选属性」在样式源里各是一条独立规则（与迁移前「一处可选属性 = 一次 b.Add」对应），
// 未配置时变量为空、该规则的声明全被省略，于是整条规则不产出。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	spacing := p.Spacing
	if spacing == "" {
		spacing = "10px"
	}
	// 对齐：仅 center / right 产出（且 CSS 的 flex 对齐值与配置值不同名）。
	alignItems := ""
	switch p.Align {
	case "center":
		alignItems = "center"
	case "right":
		alignItems = "flex-end"
	}

	vars := map[string]string{
		"spacing":          spacing,
		"icon_color":       p.IconColor,
		"icon_bg":          p.IconBgColor,
		"icon_color_hover": p.IconColorHover,
		"icon_bg_hover":    p.IconBgColorHover,
		"text_color":       p.TextColor,
		"text_size":        p.TextSize,
		"link_color":       p.LinkColor,
		"link_color_hover": p.LinkColorHover,
		"icon_size":        p.IconSize,
		"align_items":      alignItems,
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, listCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("list 组件样式解析失败: %v", err))
	}
}

// Package infobox 实现 core.infobox：信息框组件（对标 WD wd_infobox）。
// 图标（或媒体图）+ 标题 + 文本 + 可选链接，常用于服务/卖点卡片。
package infobox

import (
	_ "embed" // infobox.css 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.infobox"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("infobox", infoboxTemplate)
}

// Component 信息框组件（原子）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// Translatable 实现 core.TranslatableProvider：可翻译字段白名单（多语言 P5b，
// docs/06-D §7.5 决策 F6）。只有这里列出的字段参与内容翻译，未声明字段永不翻译。
//
// mediaAlt 的遗漏（审计 I18N-008）教训：白名单是**手工维护**的，新加一个带文本的
// Prop 时没人会记得回来补一行，而症状是「英文站的图片 alt 还是中文」——
// 只在非默认语言站点上可见，默认语言站点完全看不出来。
func (c *Component) Translatable() []string { return []string{"title", "text", "mediaAlt"} }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

// Props 信息框属性。
type Props struct {
	// Icon 内置图标名（check/star/arrow/shield/truck/cross 等；与 MediaImage 二选一）。
	Icon string `json:"icon,omitempty" ct:"select,,check=对勾,star=星形,arrow=箭头,shield=盾牌,truck=卡车,cross=叉形,sec=content,label=图标"`
	// MediaImage 媒体图 URL（与 Icon 二选一，优先于 Icon）。
	MediaImage string `json:"mediaImage,omitempty" ct:"media,sec=content,label=图片"`
	// MediaAlt 媒体图替代文本：留空表示装饰性图片（alt=""，读屏跳过）。
	// 用图标做点缀时留空是对的；放真有信息量的图（图表、产品照）时必须填。
	MediaAlt string `json:"mediaAlt,omitempty" ct:"text,maxlen=200,sec=content,label=图片替代文本"`
	// Loading 图片加载策略三态：空=默认（继承主题「图片管理」）/ on=开启懒加载 / off=关闭。
	Loading string `json:"loading,omitempty" ct:"select,=默认（继承主题）,on=开启懒加载,off=关闭懒加载,lazy=懒加载（旧）,eager=立即加载（旧）,default=,sec=content,label=图片加载"`
	// FetchPriority 资源提示优先级：空=auto（不输出属性）/ high=首屏优先 / low=次要。
	FetchPriority string `json:"fetchPriority,omitempty" ct:"select,=自动,high=高优先,low=低优先,default=,sec=content,label=加载优先级"`
	// Title 标题。
	Title string `json:"title,omitempty" ct:"text,maxlen=200,sec=content,label=标题"`
	// Text 描述文本（富文本 HTML 片段，构建期白名单清洗；存量纯文本转义后按段落包装）。
	Text string `json:"text,omitempty" ct:"richtext,maxlen=2000,sec=content,label=描述"`
	// Link 整卡链接（可选）。
	Link string `json:"link,omitempty" ct:"url,sec=content,label=链接"`
	// IconColor 图标颜色。
	IconColor string `json:"iconColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标颜色"`
	// TitleColor 标题颜色。
	TitleColor string `json:"titleColor,omitempty" ct:"color,maxlen=200,sec=style,label=标题颜色"`
	// TextColor 文本颜色。
	TextColor string `json:"textColor,omitempty" ct:"color,maxlen=200,sec=style,label=文本颜色"`
	// Align 内容对齐：left / center / right。
	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=center,sec=style,label=对齐"`
	// IconSize 图标尺寸（px，默认 40）。
	IconSize string `json:"iconSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=图标尺寸"`
	// Padding 内边距（CSS 简写）。
	Padding string `json:"padding,omitempty" ct:"margin,maxlen=30,sec=style,label=内边距"`
	// Background 卡片背景色。
	Background string `json:"background,omitempty" ct:"color,maxlen=200,sec=style,label=背景色"`
	// HoverBg 悬停背景色。
	HoverBg string `json:"hoverBg,omitempty" ct:"color,maxlen=200,sec=style,label=悬停背景色"`
	// Radius 圆角。
	Radius string `json:"radius,omitempty" ct:"dimension,maxlen=30,sec=style,label=圆角"`
	// Subtitle 副标题（图标与标题之间）。
	Subtitle string `json:"subtitle,omitempty" ct:"text,maxlen=200,sec=content,label=副标题"`
	// SubtitleColor 副标题颜色。
	SubtitleColor string `json:"subtitleColor,omitempty" ct:"color,maxlen=200,sec=style,label=副标题颜色"`
	// IconBgColor 图标背景色。
	IconBgColor string `json:"iconBgColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标背景色"`
	// IconBorderColor 图标边框色。
	IconBorderColor string `json:"iconBorderColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标边框色"`
	// TitleTag 标题标签（h2/h3/h4/div，默认 h3）。
	TitleTag string `json:"titleTag,omitempty" ct:"select,h2=H2,h3=H3,h4=H4,div=区块,default=h3,sec=style,label=标题标签"`
	// BtnText 按钮文字（与 Link 配合）。
	BtnText string `json:"btnText,omitempty" ct:"safe,maxlen=100,sec=content,label=按钮文字"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Validate 校验。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 信息框为原子组件，不允许子节点", node.ID)
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if p.Icon != "" {
		if _, ok := core.IconSVG(p.Icon); !ok {
			return fmt.Errorf("节点 %s: 未知图标 %q", node.ID, p.Icon)
		}
	}
	switch p.Align {
	case "", "left", "center", "right":
	default:
		return fmt.Errorf("节点 %s: 无效的对齐 %q", node.ID, p.Align)
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
	if err = core.ValidateSpec(&p, node.ID); err != nil {
		return err
	}
	return nil
}

// infoboxCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组。
//
//go:embed infobox.css
var infoboxCSS string

// compileCSS 信息框样式。
//
// Go 侧只做三件事：对齐档位映射、可选值兜底、把「有没有」翻成布尔量。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	align := p.Align
	if align == "" {
		align = "center"
	}
	alignItems, textAlign := "center", "center"
	switch align {
	case "left":
		alignItems, textAlign = "flex-start", "left"
	case "right":
		alignItems, textAlign = "flex-end", "right"
	}
	iconSize := p.IconSize
	if iconSize == "" {
		iconSize = "40px"
	}

	vars := map[string]string{
		"alignItems":      alignItems,
		"textAlign":       textAlign,
		"padding":         p.Padding,
		"background":      p.Background,
		"iconSize":        iconSize,
		"iconColor":       p.IconColor,
		"titleColor":      p.TitleColor,
		"textColor":       p.TextColor,
		"subtitleColor":   p.SubtitleColor,
		"radius":          p.Radius,
		"hoverBg":         p.HoverBg,
		"iconBgColor":     p.IconBgColor,
		"iconBorderColor": p.IconBorderColor,
		"hasSubtitle":     core.BoolVar(p.Subtitle != ""),
		"btnText":         core.BoolVar(p.BtnText != ""),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, infoboxCSS, vars); err != nil {
		panic(fmt.Sprintf("infobox 组件样式解析失败: %v", err))
	}
}

// infoboxTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed infobox.jet
var infoboxTemplate string

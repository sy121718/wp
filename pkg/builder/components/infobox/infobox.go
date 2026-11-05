package infobox

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"

	"go_wp/pkg/builder/core"
)

const Type = "core.infobox"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("infobox", infoboxTemplate)
}

type Component struct{}

func (c *Component) Type() string { return Type }

func (c *Component) Translatable() []string { return []string{"title", "text", "mediaAlt"} }

func (c *Component) PropsSpec() any { return &Props{} }

func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"icon":  "shield",
			"title": "信息框标题",
			"text":  "一句话描述你的服务或卖点。",
			"align": "center",
		},
		DisplayName: "信息框",
		Hint:        "图标+标题+文本",
	}
}

type Props struct {
	Icon string `json:"icon,omitempty" ct:"select,,check=对勾,star=星形,arrow=箭头,shield=盾牌,truck=卡车,cross=叉形,sec=content,label=图标"`

	MediaImage string `json:"mediaImage,omitempty" ct:"media,sec=content,label=图片"`

	MediaAlt string `json:"mediaAlt,omitempty" ct:"text,maxlen=200,sec=content,label=图片替代文本"`

	Loading string `json:"loading,omitempty" ct:"select,=默认（继承主题）,on=开启懒加载,off=关闭懒加载,lazy=懒加载（旧）,eager=立即加载（旧）,default=,sec=content,label=图片加载"`

	FetchPriority string `json:"fetchPriority,omitempty" ct:"select,=自动,high=高优先,low=低优先,default=,sec=content,label=加载优先级"`

	Title string `json:"title,omitempty" ct:"text,maxlen=200,sec=content,label=标题"`

	Text string `json:"text,omitempty" ct:"richtext,maxlen=2000,sec=content,label=描述"`

	Link string `json:"link,omitempty" ct:"url,sec=content,label=链接"`

	IconColor string `json:"iconColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标颜色"`

	TitleColor string `json:"titleColor,omitempty" ct:"color,maxlen=200,sec=style,label=标题颜色"`

	TextColor string `json:"textColor,omitempty" ct:"color,maxlen=200,sec=style,label=文本颜色"`

	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=center,sec=style,label=对齐"`

	IconSize string `json:"iconSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=图标尺寸"`

	Padding string `json:"padding,omitempty" ct:"margin,maxlen=30,sec=style,label=内边距"`

	Background string `json:"background,omitempty" ct:"color,maxlen=200,sec=style,label=背景色"`

	HoverBg string `json:"hoverBg,omitempty" ct:"color,maxlen=200,sec=style,label=悬停背景色"`

	Radius string `json:"radius,omitempty" ct:"dimension,maxlen=30,sec=style,label=圆角"`

	Subtitle string `json:"subtitle,omitempty" ct:"text,maxlen=200,sec=content,label=副标题"`

	SubtitleColor string `json:"subtitleColor,omitempty" ct:"color,maxlen=200,sec=style,label=副标题颜色"`

	IconBgColor string `json:"iconBgColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标背景色"`

	IconBorderColor string `json:"iconBorderColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标边框色"`

	TitleTag string `json:"titleTag,omitempty" ct:"select,h2=H2,h3=H3,h4=H4,div=区块,default=h3,sec=style,label=标题标签"`

	Label string `json:"label,omitempty" ct:"safe,maxlen=100,sec=content,label=按钮文字"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var infoboxLegacyKeys = []core.LegacyKey{
	{From: "btnText", To: "label"},
}

func (p *Props) NormalizeLegacy(raw json.RawMessage) error {
	return core.ApplyLegacyKeysInto(raw, p, infoboxLegacyKeys)
}

func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 信息框为原子组件，不允许子节点", node.ID)
	}

	p, derr := core.DecodeProps[Props](node.Props)
	if derr != nil {
		return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, derr)
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

//go:embed infobox.css
var infoboxCSS string

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
		"label":           core.BoolVar(p.Label != ""),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, infoboxCSS, vars); err != nil {
		panic(fmt.Sprintf("infobox 组件样式解析失败: %v", err))
	}
}

//go:embed infobox.jet
var infoboxTemplate string

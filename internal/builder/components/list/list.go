package list

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/internal/builder/core"
)

const Type = "core.list"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("list", listTemplate)
}

type Component struct{}

func (c *Component) Type() string { return Type }

func (c *Component) Translatable() []string { return []string{"text"} }

func (c *Component) PropsSpec() any { return &Props{} }

func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"style": "icon",
			"items": []any{
				map[string]any{
					"icon": "check",
					"text": "列表项内容",
				},
			},
		},
		DisplayName: "列表",
		Hint:        "图标/序号/圆点列表",
	}
}

type Item struct {
	Icon string `json:"icon,omitempty"`

	Text string `json:"text,omitempty"`

	Link string `json:"link,omitempty"`
}

type Style string

const (
	StyleIcon   Style = "icon"
	StyleNumber Style = "number"
	StyleDot    Style = "dot"
)

type Props struct {
	Items []Item `json:"items,omitempty"`

	Style Style `json:"style,omitempty" ct:"select,icon=图标,number=序号,dot=圆点,default=icon,sec=content,label=列表样式"`

	IconColor string `json:"iconColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标颜色"`

	IconSize string `json:"iconSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=图标尺寸"`

	TextColor string `json:"textColor,omitempty" ct:"color,maxlen=200,sec=style,label=文本颜色"`

	TextSize string `json:"textSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=文本字号"`

	LinkColor string `json:"linkColor,omitempty" ct:"color,maxlen=200,sec=style,label=链接颜色"`

	LinkColorHover string `json:"linkColorHover,omitempty" ct:"color,maxlen=200,sec=style,label=链接悬停颜色"`

	IconBgColor string `json:"iconBgColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标背景色"`

	IconBgColorHover string `json:"iconBgColorHover,omitempty" ct:"color,maxlen=200,sec=style,label=图标悬停背景色"`

	IconColorHover string `json:"iconColorHover,omitempty" ct:"color,maxlen=200,sec=style,label=图标悬停颜色"`

	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=left,sec=style,label=对齐"`

	Spacing string `json:"spacing,omitempty" ct:"dimension,maxlen=20,sec=style,label=项间距"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

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

//go:embed list.css
var listCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	spacing := p.Spacing
	if spacing == "" {
		spacing = "10px"
	}

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

		panic(fmt.Sprintf("list 组件样式解析失败: %v", err))
	}
}

//go:embed list.jet
var listTemplate string

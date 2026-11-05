package nav

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"html/template"
	"strings"

	"go_wp/pkg/builder/core"
)

const Type = "core.nav"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("nav", navTemplate)
}

type Component struct{}

func (c *Component) Type() string { return Type }

func (c *Component) Translatable() []string { return []string{"label"} }

func (c *Component) PropsSpec() any { return &Props{} }

func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"items": []any{
				map[string]any{
					"label": "首页",
					"url":   "/",
				},
				map[string]any{
					"label": "产品",
					"url":   "/shop",
					"children": []any{
						map[string]any{
							"label": "一次性",
							"url":   "/shop/disposable",
						},
						map[string]any{
							"label": "换弹",
							"url":   "/shop/pods",
						},
					},
				},
				map[string]any{
					"label": "关于我们",
					"url":   "/about",
				},
			},
			"orientation":    "horizontal",
			"gap":            "24px",
			"color":          "#3B3C40",
			"hoverColor":     "#D93425",
			"itemPadding":    "8px 0",
			"mobileCollapse": true,
		},
		DisplayName: "导航菜单",
		Hint:        "站点菜单（支持二级）",
	}
}

type Item struct {
	Label    string `json:"label,omitempty"`
	URL      string `json:"url,omitempty"`
	Target   string `json:"target,omitempty"`
	Children []Item `json:"children,omitempty"`

	PanelBlockID string `json:"panelBlockId,omitempty"`

	PanelWidth string `json:"panelWidth,omitempty"`

	PanelHTML template.HTML `json:"-"`

	Current bool `json:"-"`
}

type Props struct {
	Menu string `json:"menu,omitempty" ct:"select,=自定义菜单,header=页眉导航（桌面）,header_mobile=页眉导航（移动端）,footer=页脚导航（桌面）,footer_mobile=页脚导航（移动端）,sec=content,label=导航位置"`

	Navigation string `json:"navigation,omitempty" ct:"entityref,navigation,label=具体菜单项"`

	Items []Item `json:"items,omitempty"`

	Orientation string `json:"orientation,omitempty" ct:"select,horizontal=水平,vertical=垂直,sec=style,label=排列方向"`
	Gap         string `json:"gap,omitempty" ct:"dimension,maxlen=20,sec=style,label=项间距"`
	Align       string `json:"align,omitempty" ct:"select,left=靠左,center=居中,right=靠右,sec=style,label=整体对齐"`

	Color       string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=文字色"`
	HoverColor  string `json:"hoverColor,omitempty" ct:"color,maxlen=200,sec=style,label=悬停文字色"`
	ActiveColor string `json:"activeColor,omitempty" ct:"color,maxlen=200,sec=style,label=当前项颜色"`
	FontSize    string `json:"fontSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=字号"`
	FontWeight  string `json:"fontWeight,omitempty" ct:"select,400=常规,500=中等,600=半粗,700=粗体,sec=style,label=字重"`
	ItemPadding string `json:"itemPadding,omitempty" ct:"dimension,maxlen=20,sec=style,label=项内距"`

	SubmenuBg    string `json:"submenuBg,omitempty" ct:"color,maxlen=200,sec=style,label=子菜单底色"`
	SubmenuWidth string `json:"submenuWidth,omitempty" ct:"dimension,maxlen=20,sec=style,label=子菜单最小宽度"`

	MobileCollapse bool   `json:"mobileCollapse,omitempty" ct:"bool,sec=style,label=移动端折叠为汉堡菜单"`
	ToggleLabel    string `json:"toggleLabel,omitempty" ct:"safe,maxlen=20,sec=style,label=折叠按钮文字"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	var p Props
	if err = json.Unmarshal(node.Props, &p); err != nil {
		return fmt.Errorf("节点 %s props 解析失败: %w", node.ID, err)
	}
	if err = validateItems(p.Items, node.ID, 0); err != nil {
		return err
	}
	if err = core.ValidateAdvanced(core.AdvancedOf(&p), node.ID, ids); err != nil {
		return err
	}
	return core.ValidateSpec(&p, node.ID)
}

func validateItems(items []Item, nodeID string, depth int) (err error) {
	if len(items) == 0 {
		return nil
	}
	if depth > 1 {
		return fmt.Errorf("节点 %s: 菜单最多支持两级", nodeID)
	}
	if len(items) > 50 {
		return fmt.Errorf("节点 %s: 菜单项过多（上限 50）", nodeID)
	}
	for i, it := range items {
		label := strings.TrimSpace(it.Label)
		if label == "" {
			return fmt.Errorf("节点 %s: 第 %d 个菜单项缺少文字", nodeID, i+1)
		}
		if len([]rune(label)) > 60 {
			return fmt.Errorf("节点 %s: 第 %d 个菜单项文字过长", nodeID, i+1)
		}
		if u := strings.TrimSpace(it.URL); u != "" {
			if len(u) > 300 || !core.IsSafeURL(u) {
				return fmt.Errorf("节点 %s: 第 %d 个菜单项链接非法: %q", nodeID, i+1, u)
			}
		}
		if it.Target != "" && it.Target != "self" && it.Target != "blank" {
			return fmt.Errorf("节点 %s: 第 %d 个菜单项打开方式非法: %q", nodeID, i+1, it.Target)
		}
		if err = validateItems(it.Children, nodeID, depth+1); err != nil {
			return err
		}
	}
	return nil
}

//go:embed nav.css
var navCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	submenuBg := p.SubmenuBg
	if submenuBg == "" {
		submenuBg = "var(--sky-c-surface, #fff)"
	}
	justify := map[string]string{"left": "flex-start", "center": "center", "right": "flex-end"}[p.Align]

	vars := map[string]string{
		"vertical":       core.BoolVar(p.Orientation == "vertical"),
		"gap":            p.Gap,
		"justify":        justify,
		"color":          p.Color,
		"fontSize":       p.FontSize,
		"fontWeight":     p.FontWeight,
		"itemPadding":    p.ItemPadding,
		"hoverColor":     p.HoverColor,
		"activeColor":    p.ActiveColor,
		"submenuBg":      submenuBg,
		"submenuWidth":   p.SubmenuWidth,
		"mobileCollapse": core.BoolVar(p.MobileCollapse),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, navCSS, vars); err != nil {
		panic(fmt.Sprintf("nav 组件样式解析失败: %v", err))
	}
}

//go:embed nav.jet
var navTemplate string

package marquee

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strconv"

	"go_wp/pkg/builder/core"
)

const Type = "core.marquee"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("marquee", marqueeTemplate)
}

type Component struct{}

func (c *Component) Type() string { return Type }

func (c *Component) PropsSpec() any { return &Props{} }

func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"speed":     12,
			"direction": "left",
			"gap":       "24px",
		},
		DisplayName: "跑马灯",
		Hint:        "无缝滚动内容",
	}
}

type Direction string

const (
	DirLeft  Direction = "left"
	DirRight Direction = "right"
)

type Props struct {
	Speed float64 `json:"speed,omitempty" ct:"number,sec=content,label=速度(s)"`

	Direction Direction `json:"direction,omitempty" ct:"select,left=向左,right=向右,default=left,sec=style,label=滚动方向"`

	PauseOnHover bool `json:"pauseOnHover,omitempty" ct:"bool,sec=style,label=悬停暂停"`

	Gap string `json:"gap,omitempty" ct:"dimension,maxlen=20,sec=content,label=间距"`

	Background string `json:"background,omitempty" ct:"color,maxlen=200,sec=style,label=背景色"`

	Padding string `json:"padding,omitempty" ct:"margin,maxlen=30,sec=style,label=内边距"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

func (c *Component) Validate(node *core.Node, ids map[string]bool) error {
	if err := c.ValidateSelf(node, ids); err != nil {
		return err
	}
	return core.ValidateChildren(node, ids)
}

func (c *Component) ValidateSelf(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) == 0 {
		return fmt.Errorf("节点 %s: 跑马灯至少需要一个内容（把组件拖入内部）", node.ID)
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	switch p.Direction {
	case "", DirLeft, DirRight:
	default:
		return fmt.Errorf("节点 %s: 无效的滚动方向 %q", node.ID, p.Direction)
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
	if err = core.ValidateSpec(&p, node.ID); err != nil {
		return err
	}
	if p.Gap != "" && !core.IsSafeCSSValue(p.Gap) {
		return fmt.Errorf("节点 %s: 无效的内容间距: %q", node.ID, p.Gap)
	}
	return nil
}

//go:embed marquee.css
var marqueeCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	speed := p.Speed
	if speed <= 0 {
		speed = 12
	}
	dir := p.Direction
	if dir == "" {
		dir = DirLeft
	}
	from, to := "0", "-50%"
	if dir == DirRight {
		from, to = "-50%", "0"
	}

	gap := p.Gap
	if gap == "" || !core.IsSafeCSSValue(gap) {
		gap = "24px"
	}

	vars := map[string]string{
		"id":           id,
		"speed":        strconv.FormatFloat(speed, 'f', -1, 64),
		"from":         from,
		"to":           to,
		"gap":          gap,
		"pauseOnHover": core.BoolVar(p.PauseOnHover),
		"background":   p.Background,
		"padding":      p.Padding,
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, marqueeCSS, vars); err != nil {
		panic(fmt.Sprintf("marquee 组件样式解析失败: %v", err))
	}
}

//go:embed marquee.jet
var marqueeTemplate string

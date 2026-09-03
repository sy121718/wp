// Package marquee 实现 core.marquee：跑马灯组件（对标 WD wd_marquee）。
// 容器型：children 即滚动内容（文本/Logo/卡片均可），渲染为无缝循环
// 双份内容 + CSS keyframes 位移动画（零 JS）。
package marquee

import (
	"encoding/json"
	"fmt"
	"strconv"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.marquee"

func init() { core.Register(&Component{}) }

// Component 跑马灯组件（结构型）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

// Direction 滚动方向。
type Direction string

const (
	DirLeft  Direction = "left"
	DirRight Direction = "right"
)

// Props 跑马灯属性。
type Props struct {
	// Speed 滚动速度（秒，单份内容位移一个自身宽度的时间；默认 12s）。
	Speed float64 `json:"speed,omitempty"`
	// Direction 滚动方向：left / right。
	Direction Direction `json:"direction,omitempty" ct:"select,left=向左,right=向右,default=left,sec=style,label=滚动方向"`
	// PauseOnHover 悬停暂停。
	PauseOnHover bool `json:"pauseOnHover,omitempty" ct:"bool,sec=style,label=悬停暂停"`
	// Gap 内容间距（px）。
	Gap string `json:"gap,omitempty"`
	// Background 背景色。
	Background string `json:"background,omitempty" ct:"color,maxlen=200,sec=style,label=背景色"`
	// Padding 内边距。
	Padding string `json:"padding,omitempty" ct:"margin,maxlen=30,sec=style,label=内边距"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Validate 校验：至少一个内容节点。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
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
	for _, child := range node.Children {
		if err = core.ValidateNode(child, ids); err != nil {
			return fmt.Errorf("节点 %s 子节点: %w", node.ID, err)
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

// compileCSS 跑马灯样式（位移动画）。
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
	from := "0"
	to := "-50%"
	if dir == DirRight {
		from = "-50%"
		to = "0"
	}

	gap := p.Gap
	if gap == "" {
		gap = "24px"
	}

	b.Add(core.BreakpointDesktop, sel, []string{
		"display: flex", "overflow: hidden",
		"white-space: nowrap",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-marquee-track", []string{
		"display: flex", "align-items: center", "flex: none",
		core.CSSDecl("gap", gap), core.CSSDecl("padding-right", gap),
		"animation: wp-marquee-" + id + " " + strconv.FormatFloat(speed, 'f', -1, 64) + "s linear infinite",
	})
	// keyframes：整个轨道位移自身宽度一半（双份内容无缝衔接）。
	b.Add(core.BreakpointDesktop, "@keyframes wp-marquee-"+id, []string{
		"from { transform: translateX(" + from + ") }",
		"to { transform: translateX(" + to + ") }",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-marquee-item", []string{
		"flex: none", "display: inline-flex", "align-items: center",
	})
	if p.PauseOnHover {
		b.Add(core.BreakpointDesktop, sel+":hover .wp-marquee-track", []string{"animation-play-state: paused"})
	}
	if p.Background != "" {
		b.Add(core.BreakpointDesktop, sel, []string{core.CSSDecl("background", p.Background)})
	}
	if p.Padding != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-marquee-track", []string{core.CSSDecl("padding-top", p.Padding), core.CSSDecl("padding-bottom", p.Padding)})
	}
}

// Package marquee 实现 core.marquee：跑马灯组件（对标 WD wd_marquee）。
// 容器型：children 即滚动内容（文本/Logo/卡片均可），渲染为无缝循环
// 双份内容 + CSS keyframes 位移动画（零 JS）。
package marquee

import (
	_ "embed" // marquee.css 经 //go:embed 打进二进制
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
	Speed float64 `json:"speed,omitempty" ct:"number,sec=content,label=速度(s)"`
	// Direction 滚动方向：left / right。
	Direction Direction `json:"direction,omitempty" ct:"select,left=向左,right=向右,default=left,sec=style,label=滚动方向"`
	// PauseOnHover 悬停暂停。
	PauseOnHover bool `json:"pauseOnHover,omitempty" ct:"bool,sec=style,label=悬停暂停"`
	// Gap 内容间距（px）。
	Gap string `json:"gap,omitempty" ct:"dimension,maxlen=20,sec=content,label=间距"`
	// Background 背景色。
	Background string `json:"background,omitempty" ct:"color,maxlen=200,sec=style,label=背景色"`
	// Padding 内边距。
	Padding string `json:"padding,omitempty" ct:"margin,maxlen=30,sec=style,label=内边距"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
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
	if p.Gap != "" && !core.IsSafeCSSValue(p.Gap) {
		return fmt.Errorf("节点 %s: 无效的内容间距: %q", node.ID, p.Gap)
	}
	return nil
}

// marqueeCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组，
// 而作用域替换、每实例帧名装配与确定性输出仍由构建期负责。
//
//go:embed marquee.css
var marqueeCSS string

// compileCSS 跑马灯样式（位移动画）。
//
// Go 侧只把方向翻译成帧的起止值、把属性翻译成变量；哪条规则存在由样式源的 @if 决定。
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
	// 间距过白名单校验：非法值退回默认（样式源不承担安全性判断）。
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

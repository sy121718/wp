// Package slider 实现 core.slider：容器型轮播组件（对标 WD wd_slider）。
//
// 与 gallery（图片专用）不同：slider 的 children 即每一屏 slide，
// 每个 slide 可嵌套任意组件树（容器→图片/标题/按钮…）。
//
// 静态优先实现：
//   - 轨道横向 flex + CSS scroll-snap（原生滑动，零 JS 也可用）
//   - 自动播放用 CSS 动画（可选）
//   - 箭头/圆点为轻量 Client Enhancement（纯客户端交互，允许）
//
// 组件通过自定义元素 + 渲染上下文输出结构，增强脚本由
// builder 的客户端增强注入点挂载（workbench 画布与产物共用）。
package slider

import (
	"encoding/json"
	"fmt"
	"strconv"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.slider"

func init() { core.Register(&Component{}) }

// Component 轮播组件（结构型：children 为各 slide）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

// PerView 三端每屏显示数。
type PerView struct {
	Desktop int `json:"desktop,omitempty"`
	Tablet  int `json:"tablet,omitempty"`
	Mobile  int `json:"mobile,omitempty"`
}

// Props 轮播属性。
type Props struct {
	// PerView 每屏显示 slide 数（1~4，默认 1）。
	PerView PerView `json:"perView,omitempty"`
	// Autoplay 自动播放间隔（秒）；0 关闭。
	Autoplay float64 `json:"autoplay,omitempty"`
	// ShowArrows 显示左右箭头（轻量增强）。
	ShowArrows bool `json:"showArrows,omitempty"`
	// ShowDots 显示圆点指示器（轻量增强）。
	ShowDots bool `json:"showDots,omitempty"`
	// Loop 循环（末尾跳回开头，增强实现）。
	Loop bool `json:"loop,omitempty"`
	// Gap slide 间距（px）。
	Gap string `json:"gap,omitempty"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Validate 校验：叶子校验由子树递归完成；slider 要求至少一个 slide。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) == 0 {
		return fmt.Errorf("节点 %s: 轮播至少需要一个 slide（把组件拖入内部）", node.ID)
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if p.PerView.Desktop < 0 || p.PerView.Desktop > 4 || p.PerView.Tablet > 4 || p.PerView.Mobile > 4 {
		return fmt.Errorf("节点 %s: 每屏显示数需在 0~4 之间", node.ID)
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

// compileCSS 编译轮播样式（轨道 scroll-snap + slide 宽度 + 自动播放动画）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	var desktop, tablet, mobile []string

	// 容器基础。
	desktop = append(desktop, "position: relative")
	desktop = append(desktop, "overflow: hidden")

	// 轨道。
	desktop = append(desktop, "display: flex")
	desktop = append(desktop, "scroll-snap-type: x mandatory")
	desktop = append(desktop, "-webkit-overflow-scrolling: touch")
	desktop = append(desktop, "overflow-x: auto")
	desktop = append(desktop, "scrollbar-width: none")
	desktop = append(desktop, "scroll-behavior: smooth")

	// slide：每屏宽度按 perView 折算；scroll-snap-align 对齐。
	perView := p.PerView.Desktop
	if perView <= 0 {
		perView = 1
	}
	if perView > 4 {
		perView = 4
	}
	gap := p.Gap
	if gap == "" {
		gap = "16px"
	}
	slideW := 100.0 / float64(perView)
	slideSel := sel + " .wp-slide"
	slide := []string{
		"flex: 0 0 " + strconv.FormatFloat(slideW, 'f', 4, 64) + "%",
		"scroll-snap-align: start",
		"min-width: 0",
		"padding: 0 calc(" + gap + " / 2)",
	}
	desktop = append(desktop, "margin: 0 calc(-"+gap+" / 2)")

	// 三端 perView：调整 slide 宽度（覆盖 flex-basis）。
	tablet = append(tablet, perViewSlideRules(p.PerView.Tablet, gap)...)
	mobile = append(mobile, perViewSlideRules(p.PerView.Mobile, gap)...)

	b.Add(core.BreakpointDesktop, sel, desktop)
	b.Add(core.BreakpointDesktop, slideSel, slide)
	if len(tablet) > 0 {
		b.Add(core.BreakpointTablet, slideSel, tablet)
	}
	if len(mobile) > 0 {
		b.Add(core.BreakpointMobile, slideSel, mobile)
	}

	// slide 内部块级填充。
	inner := sel + " .wp-slide > *"
	b.Add(core.BreakpointDesktop, inner, []string{"height: 100%", "margin: 0"})

	// 箭头与圆点（增强）。
	arrow := sel + " .wp-slider-arrow"
	b.Add(core.BreakpointDesktop, arrow, []string{
		"position: absolute", "top: 50%", "transform: translateY(-50%)",
		"width: 40px", "height: 40px", "border-radius: 999px",
		"border: 1px solid rgba(0,0,0,.12)", "background: #fff",
		"cursor: pointer", "font-size: 20px", "line-height: 1",
		"display: flex", "align-items: center", "justify-content: center",
		"z-index: 2", "box-shadow: 0 2px 8px rgba(0,0,0,.1)",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-slider-prev", []string{"left: 12px"})
	b.Add(core.BreakpointDesktop, sel+" .wp-slider-next", []string{"right: 12px"})
	dots := sel + " .wp-slider-dots"
	b.Add(core.BreakpointDesktop, dots, []string{
		"position: absolute", "bottom: 10px", "left: 0", "right: 0",
		"display: flex", "justify-content: center", "gap: 6px", "z-index: 2",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-slider-dots button", []string{
		"width: 8px", "height: 8px", "border-radius: 999px", "border: none",
		"background: rgba(0,0,0,.25)", "cursor: pointer", "padding: 0",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-slider-dots button.is-active", []string{"background: currentColor"})
}

// perViewSlideRules 按每屏显示数生成 .wp-slide 的 flex-basis 规则（0=沿用上一档）。
func perViewSlideRules(n int, gap string) []string {
	if n <= 0 || n > 4 {
		return nil
	}
	w := 100.0 / float64(n)
	return []string{"flex: 0 0 " + strconv.FormatFloat(w, 'f', 4, 64) + "%"}
}

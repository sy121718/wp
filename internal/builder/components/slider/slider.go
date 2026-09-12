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
	_ "embed" // enhance.js 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"
	"strconv"

	"go_wp/internal/builder/core"
)

// enhanceJS 组件行为源。与 .go / .css / .jet 同目录：改交互不必再去 enhance.js 里找。
//
//go:embed enhance.js
var enhanceJS string

// Type 组件类型标识。
const Type = "core.slider"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("slider", sliderTemplate)
	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initSliders"},
		Feats:  []string{"data-slider"},
		Source: enhanceJS,
	})
}

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
	Autoplay float64 `json:"autoplay,omitempty" ct:"number,sec=content,label=自动播放间隔(s)"`
	// ShowArrows 显示左右箭头（轻量增强）。
	ShowArrows bool `json:"showArrows,omitempty" ct:"bool,sec=content,label=显示箭头"`
	// ShowDots 显示圆点指示器（轻量增强）。
	ShowDots bool `json:"showDots,omitempty" ct:"bool,sec=content,label=显示圆点"`
	// Loop 循环（末尾跳回开头，增强实现）。
	Loop bool `json:"loop,omitempty" ct:"bool,sec=content,label=循环播放"`
	// Gap slide 间距（px）。
	Gap string `json:"gap,omitempty" ct:"dimension,maxlen=20,sec=content,label=间距"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Validate 校验：叶子校验由子树递归完成；slider 要求至少一个 slide。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) == 0 {
		return fmt.Errorf("%w: 节点 %s: 轮播至少需要一个 slide（把组件拖入内部）", core.ErrIncompleteNode, node.ID)
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

// sliderCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组。
//
//go:embed slider.css
var sliderCSS string

// compileCSS 编译轮播样式（轨道 scroll-snap + slide 宽度）。
//
// Go 侧只把 perView 折算成 flex-basis 百分比、补齐间距默认值；哪一档存在由样式源的
// 媒体查询 + 空变量省略决定（这端没设 slidesPerView 时变量为空，整段不产出）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	gap := p.Gap
	if gap == "" {
		gap = "16px"
	}
	// 每屏宽度：下限 1 屏、上限 4 屏（再多每屏挤不下东西）。
	perView := p.PerView.Desktop
	if perView <= 0 {
		perView = 1
	}
	if perView > 4 {
		perView = 4
	}
	slideW := func(n int) string {
		if n <= 0 || n > 4 {
			return "" // 该端沿用上一档
		}
		return strconv.FormatFloat(100.0/float64(n), 'f', 4, 64) + "%"
	}

	vars := map[string]string{
		"gap":          gap,
		"slideDesktop": slideW(perView),
		"slideTablet":  slideW(p.PerView.Tablet),
		"slideMobile":  slideW(p.PerView.Mobile),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, sliderCSS, vars); err != nil {
		panic(fmt.Sprintf("slider 组件样式解析失败: %v", err))
	}
}

// sliderTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed slider.jet
var sliderTemplate string

// Package cardfan 实现 core.cardfan 扇形卡片墙组件：N 张绝对定位堆叠的卡片，
// 悬停时按位置偏移逐张旋转/平移展开成弧形（扑克牌扇开效果），按压时置顶。
//
// 设计取舍：这是「特定结构的组合模式」，不是通用效果词汇 —— 展开角度/平移/色相
// 都是**位置派生**的（第 i 张 = 中间对称偏移），故不放进 HoverEffect 枚举，
// 而是组件内用 :nth-child(N) 逐条生成（与 loader 九形态同构，见 effects.go 分类目录）。
// 色相用 core.AdvancedProps.HueRotate 之外的组件内 hue-rotate：这里需要「每张卡不同」
// 的偏移，通用 HueRotate 是单元素整值，语义不同。
package cardfan

import (
	"fmt"
	"strconv"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.cardfan"

// Props 扇形卡片墙属性。
type Props struct {
	// Count 卡片数量（2~12，默认 9）。卡片内容为 1~N 数字。
	Count int `json:"count,omitempty" ct:"int,min=2,max=12,sec=content,label=卡片数量"`
	// Width 单张卡片宽度（默认 240px）。
	Width string `json:"width,omitempty" ct:"dimension,maxlen=20,sec=content,label=卡片宽度"`
	// Height 单张卡片高度（默认 320px）。
	Height string `json:"height,omitempty" ct:"dimension,maxlen=20,sec=content,label=卡片高度"`
	// HueStep 相邻卡片色相步长（deg，0~120，默认 50）：中间卡 0 偏移，两侧按 ±step 渐变。
	HueStep int `json:"hueStep,omitempty" ct:"int,min=0,max=120,sec=style,label=色相步长(deg)"`
	// SpreadAngle 悬停展开角度系数（deg，0~15，默认 5）：第 i 张 rotate(i*angle)。
	SpreadAngle int `json:"spreadAngle,omitempty" ct:"int,min=0,max=15,sec=motion,label=展开角度(deg)"`
	// SpreadDistance 悬停展开平移系数（px，0~200，默认 120）：第 i 张 translate(i*dist)。
	SpreadDistance int `json:"spreadDistance,omitempty" ct:"int,min=0,max=200,sec=motion,label=展开平移(px)"`
	// Advanced 通用高级属性（基座自动校验与编译）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
	},
}

// init 注册组件。
func init() {
	core.Register(Widget)
}

// effectiveCount 空值缺省 9。
func effectiveCount(p *Props) int {
	if p.Count < 2 {
		return 9
	}
	return p.Count
}

// effectiveHueStep 空值缺省 50（负数视作 0）。
func effectiveHueStep(p *Props) int {
	if p.HueStep <= 0 {
		return 50
	}
	return p.HueStep
}

// effectiveSpreadAngle 空值缺省 5。
func effectiveSpreadAngle(p *Props) int {
	if p.SpreadAngle <= 0 {
		return 5
	}
	return p.SpreadAngle
}

// effectiveSpreadDistance 空值缺省 120。
func effectiveSpreadDistance(p *Props) int {
	if p.SpreadDistance <= 0 {
		return 120
	}
	return p.SpreadDistance
}

// validateExtra 校验：尺寸为 CSS 安全值（数量/角度/色相/平移由 ct tag min/max 兜底）。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.Width != "" && !core.IsSafeCSSValue(p.Width) {
		return fmt.Errorf("节点 %s: 无效的卡片宽度: %q", nodeID, p.Width)
	}
	if p.Height != "" && !core.IsSafeCSSValue(p.Height) {
		return fmt.Errorf("节点 %s: 无效的卡片高度: %q", nodeID, p.Height)
	}
	return nil
}

// compileCSS 容器 + 每张卡的堆叠/色相/悬停展开/按压置顶样式。
//
// 关键：色相、展开角度、平移都是位置派生（offset = i - (count-1)/2），
// 用 :nth-child(N) 逐条生成（不是 inline style 的 --i），保证：
//   - 增删卡片 CSS 自动重算（不重写 props）；
//   - 产物字节确定性（同 props 同字节）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	count := effectiveCount(p)
	mid := float64(count-1) / 2.0
	width, height := p.Width, p.Height
	if width == "" {
		width = "240px"
	}
	if height == "" {
		height = "320px"
	}
	hueStep := float64(effectiveHueStep(p))
	angle := float64(effectiveSpreadAngle(p))
	dist := float64(effectiveSpreadDistance(p))

	// 容器：相对定位，卡片绝对堆叠于中心。
	b.Add(core.BreakpointDesktop, sel, []string{
		"position: relative",
		"width: 100%",
		"display: flex",
		"justify-content: center",
		"align-items: center",
		"min-height: " + height,
	})

	for i := 0; i < count; i++ {
		offset := float64(i) - mid // -4..0..4
		nth := strconv.Itoa(i + 1)
		card := sel + " .wp-cardfan-card:nth-child(" + nth + ")"

		// 基础：绝对堆叠 + 色相偏移 + 透明数字（悬停才显现）。
		b.Add(core.BreakpointDesktop, card, []string{
			"position: absolute",
			"width: " + width,
			"height: " + height,
			"display: flex",
			"justify-content: center",
			"align-items: center",
			"background-color: #5e5cfc",
			fmt.Sprintf("filter: hue-rotate(%.0fdeg)", offset*hueStep),
			"border: 10px solid rgba(0,0,0,.1)",
			"border-radius: 8px",
			"box-shadow: 0 15px 50px rgba(0,0,0,.1)",
			"color: rgba(0,0,0,0)",
			"font-size: 8em",
			"font-weight: 700",
			"transition: .5s",
			"cursor: pointer",
			"user-select: none",
		})

		// 悬停展开：旋转 + 平移 + 文字显现 + 阴影加深。
		hover := sel + ":hover .wp-cardfan-card:nth-child(" + nth + ")"
		b.AddHover(hover, []string{
			fmt.Sprintf("transform: rotate(%.0fdeg) translate(%.0fpx, -50px)", offset*angle, offset*dist),
			"color: rgba(0,0,0,.25)",
			"box-shadow: 0 15px 50px rgba(0,0,0,.25)",
		})
	}

	// 按压：容器按下时全部卡变灰；被点的卡恢复原色并置顶。
	// 走 AddActive（不包 hover:hover）：触屏按下同样触发。
	b.AddActive(sel+":active .wp-cardfan-card", []string{"background-color: #333"})
	b.AddActive(sel+" .wp-cardfan-card:active", []string{"background-color: #5e5cfc", "z-index: 100"})
}

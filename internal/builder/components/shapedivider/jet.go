// Package shapedivider — Jet 渲染路径辅助导出。
//
// 与其他 Atom 基座组件同构：props 解码 / CSS 生成 / path 与层视图预计算保留在 Go，
// HTML 拼装交给 shapedivider.jet 模板（多层 range 输出 <path>）。
package shapedivider

import (
	"strconv"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出形状分隔线样式编译（atomViewOf 接线用）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// LayerView 单层 path 渲染数据（模板输出 <path> 属性）。
type LayerView struct {
	// Class 动画层 class（drift 时中层 sd-l2 / 背景层 sd-l3；前景层为空）。
	Class string
	// D path 数据（整数参数化生成，确定性输出）。
	D string
	// Fill 填充色（前景层 currentColor 或自定义；背景层 ColorBack 或随前景）。
	Fill string
	// FillOpacity 填充不透明度（空 = 1；背景层未显式给色时递减做景深）。
	FillOpacity string
}

// View shapedivider 渲染视图数据（供 shapedivider.jet 模板使用）。
type View struct {
	// Layers 由前到后的层序列（1~3 层，顺序固定保证确定性）。
	Layers []LayerView
}

// BuildView 生成形状分隔线渲染视图：形状 path × 层数（× 漂移扩宽）。
func BuildView(p *Props) View {
	shape := effectiveShape(p)
	layers := effectiveLayers(p)
	drift := p.Animate == AnimDrift

	v := View{Layers: make([]LayerView, 0, layers)}
	for i := range layers {
		lv := LayerView{
			D: shapePath(shape, i, drift),
		}
		if i == 0 {
			// 前景层：自定义色或 currentColor（跟随文字色，主题变量联动）。
			lv.Fill = frontFill(p.Color)
		} else {
			if p.ColorBack != "" {
				lv.Fill = p.ColorBack
			} else {
				lv.Fill = frontFill(p.Color)
				lv.FillOpacity = backOpacity(i)
			}
			if drift {
				lv.Class = "sd-l" + strconv.Itoa(i+1)
			}
		}
		v.Layers = append(v.Layers, lv)
	}
	return v
}

// frontFill 前景填充（空 = currentColor）。
func frontFill(color string) string {
	if color == "" {
		return "currentColor"
	}
	return color
}

// backOpacity 背景层缺省不透明度（同色渐淡做景深；layer 为 0 起层索引）。
func backOpacity(layer int) string {
	if layer >= 2 {
		return "0.3"
	}
	return "0.55"
}

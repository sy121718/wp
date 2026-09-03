// Package slider — Jet 渲染路径辅助导出（Phase 2）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / 属性与 slide 轨道预计算
// 保留在 Go，HTML 拼装交给 slider.jet 模板（children 递归 include）。
// Render 方法保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package slider

import (
	"html"
	"strconv"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出轮播样式编译（复用 Render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View slider 渲染视图数据（供 slider.jet 模板使用）。
type View struct {
	// DataSlider data-slider 属性值（节点 ID，已转义）。
	DataSlider string
	// HasAutoplay 是否输出 data-autoplay 属性（Autoplay > 0）。
	HasAutoplay bool
	// Autoplay 自动播放间隔（秒，序列化字符串）。
	Autoplay string
	// Loop 循环开关（data-loop="1"）。
	Loop bool
	// ShowArrows 显示左右箭头。
	ShowArrows bool
	// ShowDots 显示圆点指示器。
	ShowDots bool
}

// BuildView 生成轮播渲染视图：data-slider/data-autoplay/data-loop 属性预计算
// （与 Render 输出结构一致）。children 的递归渲染由 nodeView 层驱动。
func BuildView(node *core.Node, p *Props) View {
	v := View{
		DataSlider: html.EscapeString(node.ID),
		Loop:       p.Loop,
		ShowArrows: p.ShowArrows,
		ShowDots:   p.ShowDots,
	}
	if p.Autoplay > 0 {
		v.HasAutoplay = true
		v.Autoplay = strconv.FormatFloat(p.Autoplay, 'f', -1, 64)
	}
	return v
}

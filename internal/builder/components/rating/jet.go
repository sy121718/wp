// Package rating — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成 / 星形填充状态列表预计算
// 保留在 Go，HTML 拼装交给 rating.jet 模板。render 函数保持不变（旧输出），
// 本文件只做最小导出与等价的数据准备。
package rating

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出评分样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// 星形填充形态（供 rating.jet 模板选择 <svg> 骨架）。
const (
	starFormFull  = "full"  // 实心
	starFormHalf  = "half"  // 半星（左半填充）
	starFormEmpty = "empty" // 空星
)

// StarView 单颗星渲染视图（供 rating.jet 模板使用）。
type StarView struct {
	// Form 星形形态（full/half/empty），模板据此选择 <svg> 骨架。
	Form string
	// Points 星形 polygon 顶点（starPoints，模板填充 points 属性）。
	Points string
}

// View rating 渲染视图数据（供 rating.jet 模板使用）。
type View struct {
	// Stars 星形列表（长度 = effectiveMax）。
	Stars []StarView
	// Label 无障碍描述文本（role=img 的 aria-label）。
	Label string
}

// BuildView 生成评分渲染视图：按 Value/Max 计算每颗星填充状态（实心/半星/空星）。
func BuildView(p *Props) View {
	max := effectiveMax(p)
	full := fullCount(p)
	half := hasHalf(p)

	stars := make([]StarView, 0, max)
	for i := 0; i < max; i++ {
		var form string
		switch {
		case i < full:
			form = starFormFull
		case i == full && half:
			form = starFormHalf
		default:
			form = starFormEmpty
		}
		stars = append(stars, StarView{Form: form, Points: starPoints})
	}
	return View{Stars: stars, Label: ratingLabel(p)}
}

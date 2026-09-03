// Package marquee — Jet 渲染路径辅助导出（Phase 2）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / 双份内容副本序号预计算
// 保留在 Go，HTML 拼装交给 marquee.jet 模板（双份 track 循环 + children 递归 include）。
// Render 方法保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package marquee

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出跑马灯样式编译（复用 Render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View marquee 渲染视图数据（供 marquee.jet 模板使用）。
type View struct {
	// PauseOnHover 悬停暂停（data-pause-on-hover="1"）。
	PauseOnHover bool
	// Copies 双份内容副本序号（data-copy 取值 0/1，无缝循环）。
	Copies []int
}

// BuildView 生成跑马灯渲染视图：双份内容副本序号预计算（与 Render 输出结构一致）。
// children 的递归渲染由 nodeView 层驱动（两份内容各渲染一次）。
func BuildView(p *Props) View {
	return View{PauseOnHover: p.PauseOnHover, Copies: []int{0, 1}}
}

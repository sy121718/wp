// Package progress — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成 / aria 属性预计算保留在 Go，
// HTML 拼装交给 progress.jet 模板。render 函数保持不变（旧输出），
// 本文件只做最小导出与等价的数据准备。
package progress

import (
	"strconv"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出进度条样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View progress 渲染视图数据（供 progress.jet 模板使用）。
type View struct {
	// ValueNow aria-valuenow（当前值）。
	ValueNow string
	// ValueMax aria-valuemax（上限，已应用缺省 100）。
	ValueMax string
	// Label 标签文字（模板输出时由 Jet 默认转义）。
	Label string
	// HasLabel 是否输出标签。
	HasLabel bool
}

// BuildView 生成进度条渲染视图：aria 属性 + 标签。
func BuildView(p *Props) View {
	return View{
		ValueNow: strconv.Itoa(p.Value),
		ValueMax: strconv.Itoa(effectiveMax(p)),
		Label:    p.Label,
		HasLabel: p.Label != "",
	}
}

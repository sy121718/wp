// Package counter — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / 数字格式化与增强属性
// 保留在 Go，HTML 拼装交给 counter.jet 模板。Render 方法保持不变（旧输出），
// 本文件只做最小导出与等价的数据准备。
package counter

import (
	"strconv"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出计数器样式编译（复用 Render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View counter 渲染视图数据（供 counter.jet 模板使用）。
type View struct {
	// StartData / EndData / DecimalsData / DurationData 增强 data 属性值（数字串）。
	StartData    string
	EndData      string
	DecimalsData string
	DurationData string
	// Value 无 JS 时的结束值（最终态）。
	Value string
	// Prefix / Suffix / Label 前缀/后缀/标签（模板输出时由 Jet 默认转义）。
	Prefix string
	Suffix string
	Label  string
}

// BuildView 生成计数器渲染视图：数字格式化 + 增强 data 属性。
func BuildView(p *Props) View {
	duration := p.Duration
	if duration <= 0 {
		duration = 2
	}
	return View{
		StartData:    strconv.FormatFloat(p.Start, 'f', -1, 64),
		EndData:      strconv.FormatFloat(p.End, 'f', -1, 64),
		DecimalsData: strconv.Itoa(p.Decimals),
		DurationData: strconv.FormatFloat(duration, 'f', -1, 64),
		Value:        formatNum(p.End, p.Decimals),
		Prefix:       p.Prefix,
		Suffix:       p.Suffix,
		Label:        p.Label,
	}
}

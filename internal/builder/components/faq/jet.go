// Package faq — Jet 渲染路径辅助导出。
//
// 与 compileCSS 并行的新路径：props 解码 / CSS 生成保留在 Go，
// HTML 拼装交给 faq.jet 模板（details/summary 遍历 items）。
// 本文件只做最小导出与等价的数据准备。
package faq

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出常见问题样式编译（复用 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// FaqItemView 单条常见问题视图（供 faq.jet 模板使用）。
type FaqItemView struct {
	// Question 问题文本（模板输出时由 Jet 默认转义）。
	Question string
	// Answer 答案文本（模板输出时由 Jet 默认转义）。
	Answer string
	// Open 默认展开。
	Open bool
}

// View faq 渲染视图数据（供 faq.jet 模板使用）。
type View struct {
	// Items 常见问题条目（顺序一致）。
	Items []FaqItemView
}

// BuildView 生成常见问题渲染视图：问题/答案/展开态预计算。
func BuildView(p *Props) View {
	items := make([]FaqItemView, 0, len(p.Items))
	for _, it := range p.Items {
		items = append(items, FaqItemView{Question: it.Question, Answer: it.Answer, Open: it.Open})
	}
	return View{Items: items}
}

// Package heading — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成 / 内容解析与标签选择保留在 Go，
// HTML 拼装交给 heading.jet 模板。render 函数保持不变（旧输出），
// 本文件只做最小导出与等价的数据准备。
package heading

import (
	"fmt"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出标题样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View heading 渲染视图数据（供 heading.jet 模板使用）。
type View struct {
	// Tag 语义标签（h1~h6/div/span，默认 h2）。
	Tag string
	// Subtitle 副标题（空则无，模板输出时由 Jet 默认转义）。
	Subtitle string
	// Text 主标题文本（模板输出时由 Jet 默认转义）。
	Text string
	// Highlight 是否套高亮背景盒。
	Highlight bool
}

// BuildView 生成标题渲染视图：内容解析（绑定/Fallback/静态）+ 语义标签 + 高亮标志。
func BuildView(p *Props, content core.ContentResolver) (View, error) {
	tag := p.Tag
	if tag == "" {
		tag = "h2"
	}

	// 内容解析：绑定优先，空值回退 Fallback，再回退静态文本。
	text := p.Text
	if p.Binding != nil && p.Binding.Field != "" {
		if content == nil {
			return View{}, fmt.Errorf("编译上下文缺少内容解析器，无法解析绑定 %q", p.Binding.Field)
		}
		v, err := content.ResolveString(p.Binding.Field)
		if err != nil {
			return View{}, fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
		}
		if v != "" {
			text = v
		} else if p.Binding.Fallback != "" {
			text = p.Binding.Fallback
		}
	}

	return View{Tag: tag, Subtitle: p.Subtitle, Text: text, Highlight: p.HighlightColor != ""}, nil
}

// Package text — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：props 解码 / CSS 生成 / 内容解析与富文本清洗保留在 Go，
// HTML 拼装交给 text.jet 模板（纯文本转义 / 富文本原样输出）。
// render 函数保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package text

import (
	"fmt"
	"html"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出正文样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View text 渲染视图数据（供 text.jet 模板使用）。
type View struct {
	// IsPlain 是否为纯文本模式（否则富文本）。
	IsPlain bool
	// PlainTag 纯文本模式的包裹标签（p/span）。
	PlainTag string
	// Text 纯文本内容（模板输出时由 Jet 默认转义）。
	Text string
	// SanitizedContent 富文本清洗后的 HTML 片段（原样输出）。
	SanitizedContent string
}

// BuildView 生成正文渲染视图：内容解析（绑定/Fallback/静态）+ 纯文本/富文本分支。
func BuildView(p *Props, content core.ContentResolver) (View, error) {
	mode := p.Mode
	if mode == "" {
		mode = ModeRichText
	}
	plainTag := p.PlainTag
	if mode == ModePlainText && plainTag == "" {
		plainTag = "p"
	}

	// 内容解析：绑定优先，空值回退 Fallback，再回退静态内容。
	c := p.Text
	if p.Binding != nil && p.Binding.Field != "" {
		if content == nil {
			return View{}, fmt.Errorf("编译上下文缺少内容解析器，无法解析绑定 %q", p.Binding.Field)
		}
		v, err := content.ResolveString(p.Binding.Field)
		if err != nil {
			return View{}, fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
		}
		if v != "" {
			c = v
		} else if p.Binding.Fallback != "" {
			c = p.Binding.Fallback
		}
	}

	if mode == ModePlainText {
		return View{IsPlain: true, PlainTag: plainTag, Text: c}, nil
	}

	rich := sanitizeRichHTML(c)
	if p.Excerpt > 0 {
		rich = html.EscapeString(truncateRunes(stripRichTags(c), p.Excerpt))
	}
	return View{IsPlain: false, SanitizedContent: rich}, nil
}

// Package quote — Jet 渲染路径辅助导出。
//
// 与 compileCSS 并行的新路径：props 解码 / CSS 生成保留在 Go，
// HTML 拼装交给 quote.jet 模板（blockquote + p + cite）。
// 本文件只做最小导出与等价的数据准备。
package quote

import (
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出引用样式编译（复用 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View quote 渲染视图数据（供 quote.jet 模板使用）。
type View struct {
	// Text 引用内容（已由 core.RichTextHTML 处理：富文本白名单清洗 / 存量纯文本段落化，
	// 模板侧 unsafe 原样输出）。
	Text string
	// Author 作者（空则无 cite）。
	Author string
	// Source 出处链接（空则 cite 内无 <a>）。
	Source string
	// HasAuthor 是否有作者。
	HasAuthor bool
	// HasSource 是否有出处链接。
	HasSource bool
}

// BuildView 生成引用渲染视图：作者/出处可选分支。
//
// siteLink 站内链接本地化器（审计 I18N-015，可空）：出处链接若是作者手填的站内路径
// （"/about"），要按当前语言加前缀；外链由 core.ResolveSiteLink 自行放行。
func BuildView(p *Props, siteLink func(string) string) View {
	source := p.Source
	if strings.TrimSpace(source) != "" {
		source = core.SiteLinkOrSame(siteLink, source)
	}
	return View{
		Text:      core.RichTextHTML(p.Text),
		Author:    p.Author,
		Source:    source,
		HasAuthor: p.Author != "",
		HasSource: p.Source != "",
	}
}

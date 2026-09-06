// Package table — Jet 渲染路径辅助导出。
//
// 与 compileCSS 并行的新路径：props 解码 / CSS 生成保留在 Go，
// HTML 拼装交给 table.jet 模板（caption / thead / tbody 结构）。
// 本文件只做最小导出与等价的数据准备。
package table

import (
	"go_wp/internal/builder/core"
)

// CompileCSS 导出表格样式编译（复用 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View table 渲染视图数据（供 table.jet 模板使用）。
type View struct {
	// Caption 表格标题（空则无 caption）。
	Caption string
	// Headers 表头列。
	Headers []string
	// Rows 数据行（二维）。
	Rows [][]string
	// HasCaption 是否有标题。
	HasCaption bool
	// HasHeader 是否有表头。
	HasHeader bool
	// HasBody 是否有数据行。
	HasBody bool
}

// BuildView 生成表格渲染视图：表头/数据行直通，布尔标志驱动模板分支。
// 单元格文本由 Jet 默认转义（原样字符串）。
func BuildView(p *Props) View {
	return View{
		Caption:    p.Caption,
		Headers:    p.Headers,
		Rows:       p.Rows,
		HasCaption: p.Caption != "",
		HasHeader:  len(p.Headers) > 0,
		HasBody:    len(p.Rows) > 0,
	}
}

// Package list — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / repeater items 与图标预计算
// 保留在 Go，HTML 拼装交给 list.jet 模板。Render 方法保持不变（旧输出），
// 本文件只做最小导出与等价的数据准备。
package list

import (
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出列表样式编译（复用 Render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// ItemView 列表项渲染视图（供 list.jet 模板使用）。
type ItemView struct {
	// MarkerHTML 前缀（图标 SVG / 序号 / 圆点），原样输出。
	MarkerHTML string
	// IsLink 是否可链接。
	IsLink bool
	// LinkHref 链接地址（模板输出时由 Jet 默认转义）。
	LinkHref string
	// Text 文本内容（模板输出时由 Jet 默认转义）。
	Text string
}

// View list 渲染视图数据（供 list.jet 模板使用）。
type View struct {
	// Style 列表样式：icon / number / dot。
	Style string
	// Items 列表项（repeater）。
	Items []ItemView
}

// BuildView 生成列表渲染视图：repeater items + 前缀（图标/序号/圆点）预计算。
func BuildView(p *Props) View {
	style := p.Style
	if style == "" {
		style = StyleIcon
	}
	items := make([]ItemView, 0, len(p.Items))
	for i, item := range p.Items {
		iv := ItemView{Text: item.Text}
		switch style {
		case StyleIcon:
			svg, ok := core.IconSVG(item.Icon)
			if !ok {
				svg, _ = core.IconSVG("check")
			}
			iv.MarkerHTML = svg
		case StyleNumber:
			iv.MarkerHTML = fmt.Sprintf("%d", i+1)
		default: // dot
			iv.MarkerHTML = `<i class="wp-list-dot"></i>`
		}
		if strings.TrimSpace(item.Link) != "" {
			iv.IsLink = true
			iv.LinkHref = item.Link
		}
		items = append(items, iv)
	}
	return View{Style: string(style), Items: items}
}

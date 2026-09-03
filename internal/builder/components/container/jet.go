// Package container — Jet 渲染路径辅助导出（Phase 0 样板）。
//
// 与 Render 方法并行的新路径：props 解码 / CSS 生成 / 属性串与形状分隔线计算
// 保留在 Go，HTML 拼装交给 container.jet 模板（含子节点递归 include）。
// Render 方法保持不变（旧输出），本文件只做最小导出与等价的数据准备。
package container

import (
	"html"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出容器样式编译（复用 Render 内部的 compileCSS，CSS 字节与旧路径一致）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// View container 渲染视图数据（供 container.jet 模板使用）。
type View struct {
	// Tag 原生语义标签（div/section/article/aside/nav/header/footer/main）。
	Tag string
	// Attrs 前导空格 + 属性串（组父联动 / 自定义属性 / 抽屉协议，均已转义）。
	Attrs string
	// ShapeTop 顶部形状分隔线 SVG（空则无）。
	ShapeTop string
	// ShapeBottom 底部形状分隔线 SVG（空则无）。
	ShapeBottom string
}

// BuildView 生成容器渲染视图：属性串 + 形状分隔线（与 Render 输出结构一致）。
func BuildView(node *core.Node, p *Props) View {
	var attrs strings.Builder
	// 组父联动标记（03-A：子组件可经 [data-wp-group] 联动）。
	if p.StyleEx.GroupParent {
		attrs.WriteString(` data-wp-group="true"`)
	}
	// 自定义属性键值对（白名单 key + 安全 value）。
	for _, kv := range p.StyleEx.Attributes {
		attrs.WriteString(" ")
		attrs.WriteString(kv.Key)
		attrs.WriteString(`="`)
		attrs.WriteString(html.EscapeString(kv.Value))
		attrs.WriteString(`"`)
	}
	// 抽屉协议（:target 显隐，零 JS）。
	if p.Position.Type == "drawer" {
		attrs.WriteString(` id="wp-drawer-`)
		attrs.WriteString(node.ID)
		attrs.WriteString(`" data-drawer-side="`)
		attrs.WriteString(p.Position.DrawerSide)
		attrs.WriteString(`"`)
		if p.Position.DrawerOverlay {
			attrs.WriteString(` data-drawer-overlay="true"`)
		}
	}

	v := View{Tag: p.Tag, Attrs: attrs.String()}
	if p.StyleEx.ShapeDivider != "" {
		svg := shapeDividers[p.StyleEx.ShapeDivider]
		if p.StyleEx.ShapeDividerPosition == "top" {
			v.ShapeTop = svg
		} else {
			v.ShapeBottom = svg
		}
	}
	return v
}

package builder

import "go_wp/internal/builder/core"

// incompleteViewOf 保留真实节点类名，让未完成控件在画布上仍能选中继续编辑。
// 只由通过容错校验的预览调用；发布不会生成此视图。
func incompleteViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) *nodeView {
	classes := core.NodeClass(node.ID) + " sky-incomplete"
	if topLevel {
		classes += " " + core.SectionClass
	}
	ctx.CSS.Add(core.BreakpointDesktop, "."+core.NodeClass(node.ID), []string{
		"min-height: 72px", "width: 100%", "box-sizing: border-box",
		"display: flex", "align-items: center", "justify-content: center", "gap: 8px",
		"padding: 16px", "border: 1px dashed #a1a1aa", "border-radius: 8px",
		"background: #fafafa", "color: #71717a", "font: 13px/1.5 sans-serif",
	})
	return &nodeView{
		Type: node.Type, Template: "incomplete", NodeID: node.ID,
		Classes: classes, TopLevel: topLevel, Tag: "div", Text: node.Name,
		V: ctx.Text("workbench.preview.incomplete", "待完善内容"),
	}
}

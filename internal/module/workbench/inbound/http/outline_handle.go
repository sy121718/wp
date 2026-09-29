package workbenchhttp

// outline_handle.go — 结构树的服务端渲染（HTMX 化，docs/09 §3）。
//
// 背景：workbench.js 的 renderTree 用 100+ 行 DOM 代码递归建树并给每个节点绑 6 类事件。
// 本文件把「树 HTML」搬到服务端（Jet 片段），客户端只保留一次事件委托
//（选中/拖拽/右键/重命名/caret 折叠），DOM 由服务端产出。
//
// 端点：POST /workbench/outline，参数 document（草稿 JSON）+ selectedId + filter。

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"

	workbenchenums "go_wp/internal/module/workbench/enums"

	"github.com/gin-gonic/gin"
)

// outlineNode 结构树节点（文档 JSON 的子集）。
type outlineNode struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Name     string        `json:"name"`
	Hidden   bool          `json:"hidden"`
	Locked   bool          `json:"locked"`
	Children []outlineNode `json:"children"`
}

// OutlineTree 渲染结构树片段（过滤规则与前端一致：节点或任一后代命中即保留整条链路）。
func (h *Handle) OutlineTree(c *gin.Context) {
	var page struct {
		Root []outlineNode `json:"root"`
	}
	if doc := c.PostForm("document"); doc != "" {
		_ = json.Unmarshal([]byte(doc), &page)
	}
	selectedID := strings.TrimSpace(c.PostForm("selectedId"))
	filter := strings.ToLower(strings.TrimSpace(c.PostForm("filter")))
	c.HTML(http.StatusOK, "fragments/outline_tree", gin.H{
		"HTML": renderOutlineHTML(c, page.Root, selectedID, filter),
	})
}

// renderOutlineHTML 递归渲染节点树为 HTML（树结构简单，用拼串而非模板递归）。
//
// c 参与签名只为一件事：按钮提示与徽标文案要按请求语言取词（workbenchShortText）。
// 这些句子进的是 HTML 属性与文本节点，不经过 Jet 取词层 —— 见 workbenchenums 里
// workbench.outline.* 那批 key 的说明。
func renderOutlineHTML(c *gin.Context, nodes []outlineNode, selectedID, filter string) string {
	var sb strings.Builder
	writeOutlineNodes(c, &sb, nodes, selectedID, filter)
	return sb.String()
}

// writeOutlineNodes 深度优先输出 <ul><li><div class="wb-node">…</div><ul>…</ul></li>…</ul>。
func writeOutlineNodes(c *gin.Context, sb *strings.Builder, nodes []outlineNode, selectedID, filter string) {
	sb.WriteString("<ul>")
	for i := range nodes {
		n := &nodes[i]
		if filter != "" && !outlineSubtreeHit(n, filter) {
			continue
		}
		label := outlineLabel(n)
		cls := "wb-node"
		if n.ID == selectedID {
			cls += " is-selected"
		}
		sb.WriteString("<li>")
		// role=treeitem + tabindex=0：键盘可达（焦点环样式见 workbench-a11y.css，
		// 方向键/Enter 行为由客户端 bindTreeHtmx 的事件委托实现）。
		sb.WriteString(`<div class="` + cls + `" role="treeitem" tabindex="0" data-id="` + html.EscapeString(n.ID) +
			`" data-type="` + html.EscapeString(n.Type) + `" draggable="true">`)
		caret := ""
		if len(n.Children) > 0 {
			caret = "▾"
		}
		sb.WriteString(`<button class="wb-caret" title="` +
			html.EscapeString(workbenchShortText(c, workbenchenums.OutlineToggle)) + `">` + caret + `</button>`)
		// data-named 标记用户是否自定义了名称：未命名时客户端用组件中文名覆盖显示。
		sb.WriteString(`<span class="wb-node-name" data-named="` + boolFlag(n.Name != "") + `">` +
			html.EscapeString(label) + `</span>`)
		if n.Hidden {
			sb.WriteString(`<span class="wb-node-flag" title="` +
				html.EscapeString(workbenchShortText(c, workbenchenums.OutlineHiddenHint)) + `">` +
				html.EscapeString(workbenchShortText(c, workbenchenums.OutlineHiddenBadge)) + `</span>`)
		}
		if n.Locked {
			sb.WriteString(`<span class="wb-node-flag" title="` +
				html.EscapeString(workbenchShortText(c, workbenchenums.OutlineLockedHint)) + `">` +
				html.EscapeString(workbenchShortText(c, workbenchenums.OutlineLockedBadge)) + `</span>`)
		}
		sb.WriteString(`<span class="wb-node-actions">`)
		for _, op := range []struct{ text, titleKey, op string }{
			{"↑", workbenchenums.OutlineOpUp, "up"}, {"↓", workbenchenums.OutlineOpDown, "down"},
			{"⧉", workbenchenums.OutlineOpDup, "dup"}, {"✕", workbenchenums.OutlineOpDel, "del"},
		} {
			sb.WriteString(`<button type="button" class="wb-node-action" data-wb-op="` + op.op +
				`" title="` + html.EscapeString(workbenchShortText(c, op.titleKey)) + `">` + op.text + `</button>`)
		}
		sb.WriteString(`</span></div>`)
		if len(n.Children) > 0 {
			writeOutlineNodes(c, sb, n.Children, selectedID, filter)
		}
		sb.WriteString("</li>")
	}
	sb.WriteString("</ul>")
}

// outlineLabel 节点显示名：用户命名 > 组件类型（去 core. 前缀，客户端会换成中文）> 节点 ID。
func outlineLabel(n *outlineNode) string {
	if strings.TrimSpace(n.Name) != "" {
		return n.Name
	}
	if t := strings.TrimPrefix(n.Type, "core."); t != "" {
		return t
	}
	return n.ID
}

// outlineSubtreeHit 节点自身或任一后代命中过滤词。
func outlineSubtreeHit(n *outlineNode, filter string) bool {
	if strings.Contains(strings.ToLower(outlineLabel(n)), filter) {
		return true
	}
	for i := range n.Children {
		if outlineSubtreeHit(&n.Children[i], filter) {
			return true
		}
	}
	return false
}

// boolFlag 布尔转 "1"/""（模板/属性用）。
func boolFlag(v bool) string {
	if v {
		return "1"
	}
	return ""
}

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
	"net/http"
	"strings"

	workbenchservice "go_wp/internal/module/workbench/service"

	"github.com/gin-gonic/gin"
)

// OutlineTree 渲染结构树片段（过滤规则与前端一致：节点或任一后代命中即保留整条链路）。
func (h *Handle) OutlineTree(c *gin.Context) {
	var page struct {
		Root []workbenchservice.OutlineNode `json:"root"`
	}
	if doc := c.PostForm("document"); doc != "" {
		_ = json.Unmarshal([]byte(doc), &page)
	}
	selectedID := strings.TrimSpace(c.PostForm("selectedId"))
	filter := strings.ToLower(strings.TrimSpace(c.PostForm("filter")))
	c.HTML(http.StatusOK, "fragments/outline_tree", gin.H{
		"HTML": workbenchservice.RenderOutlineHTML(page.Root, selectedID, filter, workbenchTrFunc(c)),
	})
}

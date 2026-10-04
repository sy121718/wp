package workbenchhttp

import (
	"encoding/json"
	"net/http"
	"strings"

	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/internal/templates"
	"go_wp/internal/web/shell"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// InspectorPanel 渲染选中节点的检查器面板片段。
func (h *Handle) InspectorPanel(c *gin.Context) {
	nodeID := strings.TrimSpace(c.PostForm("nodeId"))
	node, err := workbenchservice.FindDocNode(json.RawMessage(c.PostForm("document")), nodeID)
	if err != nil || node == nil {
		c.HTML(http.StatusOK, "fragments/inspector_panel", gin.H{
			"NodeID": "",
			// 空态分支也要给 t：片段模板里的取词（workbench.ui.inspector.empty 等）缺 t 时
			// Jet 会静默输出空串（不是报错），空面板会变成一句话都没有。
			"t": templates.TranslateFunc(response.RequestLanguage(c)),
		})
		return
	}
	// tab：content / style（空 = 渲染全部，向后兼容旧调用）。
	tab := strings.TrimSpace(c.PostForm("tab"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	// 装配链（组件 schema 取数 → 解包 → 字段构造 → 分组桶 → 重复项面板）全在
	// service.InspectorSections：任一步取数失败都以 error 出来，本函数只决定出口形态
	//（保持 text/plain 不变 —— 前端是 fetch → r.text() → morphHTML，不检查 r.ok，
	// 换成 JSON 反而会把一段 JSON 铺进面板）。
	sections, err := h.svc.InspectorSections(c.Request.Context(), node, tab, projectID, workbenchTrFunc(c))
	if err != nil {
		c.String(http.StatusInternalServerError, shell.PageInternalText(c))
		return
	}
	c.HTML(http.StatusOK, "fragments/inspector_panel", gin.H{
		"NodeID": nodeID, "NodeType": node.Type,
		"Sections": sections,
		// 片段模板的取词函数（与后台页面同一份 TranslateFunc）：片段不经 shell.Prepare，
		// 不注入的话新增文案只能写死在模板里，英文界面上会留下中文。
		"t": templates.TranslateFunc(response.RequestLanguage(c)),
	})
}

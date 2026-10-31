package workbenchhttp

import (
	_ "embed"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	workbenchdto "go_wp/internal/module/workbench/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/internal/templates"
	"go_wp/pkg/response"
)

func (h *Handle) InspectorPanel(c *gin.Context) {
	req, err := binding.ParseForm[workbenchdto.InspectorPanelReq](c)
	if err != nil {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrPreviewParamsIncomplete))
		return
	}
	nodeID := strings.TrimSpace(req.NodeID)
	node, err := workbenchservice.FindDocNode(json.RawMessage(req.Document), nodeID)
	if err != nil || node == nil {

		pageview.RenderTemplate(c, InspectorPanelTemplate, adminui.PrepareBareView(c, &InspectorPanelResp{NodeID: ""}))
		return
	}

	tab := strings.TrimSpace(req.Tab)
	projectID := strings.TrimSpace(req.ProjectID)

	sections, err := h.svc.InspectorSections(c.Request.Context(), node, tab, projectID, workbenchTrFunc(c))
	if err != nil {
		c.String(http.StatusInternalServerError, adminui.PageInternalText(c))
		return
	}

	pageview.RenderTemplate(c, InspectorPanelTemplate, adminui.PrepareBareView(c, &InspectorPanelResp{
		NodeID: nodeID, NodeType: node.Type,
		Sections: sections,
	}))
}

func (h *Handle) OutlineTree(c *gin.Context) {
	var page struct {
		Root []workbenchservice.OutlineNode `json:"root"`
	}
	req, _ := binding.ParseForm[workbenchdto.OutlineTreeReq](c)
	if doc := req.Document; doc != "" {
		_ = json.Unmarshal([]byte(doc), &page)
	}
	selectedID := strings.TrimSpace(req.SelectedID)
	filter := strings.ToLower(strings.TrimSpace(req.Filter))

	pageview.RenderTemplate(c, OutlineTreeTemplate, adminui.PrepareBareView(c, &OutlineTreeResp{
		HTML: workbenchservice.RenderOutlineHTML(page.Root, selectedID, filter, workbenchTrFunc(c)),
	}))
}

func renderWorkbenchPageError(c *gin.Context, status int, key string) {
	renderWorkbenchPageErrorText(c, status, key, workbenchShortText(c, key))
}

func renderWorkbenchPageErrorText(c *gin.Context, status int, key, text string) {
	if strings.TrimSpace(text) == "" {
		text = workbenchShortText(c, key)
	}
	lang := response.RequestLanguage(c)
	c.Header("Cache-Control", "no-store")

	c.HTML(status, "workbench/error", adminui.PrepareBareView(c, &WorkbenchErrorPageResp{
		Lang:     lang,
		T:        templates.TranslateFunc(lang),
		Title:    text,
		Message:  text,
		BackPath: "/admin",
	}))
}

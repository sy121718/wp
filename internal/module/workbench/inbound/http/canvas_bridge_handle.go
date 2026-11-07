package workbenchhttp

import (
	_ "embed"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	workbenchdto "go_wp/internal/module/workbench/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

func (h *Handle) Preview(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("id"))
	if pageID == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, workbenchenums.ErrMissingPageID)
		return
	}
	page, err := h.svc.PageByID(c.Request.Context(), pageID)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, workbenchenums.ErrPageNotFound)
		return
	}
	h.renderPreview(c, page.DraftDocument, page.ProjectID, page.DraftPath, c.Query("editor") == "1", workbenchservice.PreviewDocPage)
}

func (h *Handle) BlockPreview(c *gin.Context) {
	blockID := strings.TrimSpace(c.Query("id"))
	if blockID == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, workbenchenums.ErrMissingBlockID)
		return
	}
	block, err := h.svc.BlockByID(c.Request.Context(), blockID)
	if err != nil || block == nil {
		response.ErrorWithMessage(c, http.StatusNotFound, workbenchenums.ErrBlockNotFound)
		return
	}
	h.renderPreview(c, block.Document, block.ProjectID, "", c.Query("editor") == "1", workbenchservice.PreviewDocBlock)
}

func (h *Handle) PreviewDraft(c *gin.Context) {
	req, _ := binding.ParseForm[workbenchdto.PreviewDraftReq](c)
	pageID := strings.TrimSpace(req.ID)
	document := json.RawMessage(req.DraftDocument)
	version, err := strconv.ParseInt(strings.TrimSpace(req.ExpectedVersion), 10, 64)
	if pageID == "" || err != nil || len(document) == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, workbenchenums.ErrDraftDecodeFailed)
		return
	}
	page, err := h.svc.PageByID(c.Request.Context(), pageID)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, workbenchenums.ErrPageNotFound)
		return
	}
	if version != page.DraftVersion {
		response.ErrorWithMessage(c, http.StatusConflict, workbenchenums.ErrDraftVersionStale)
		return
	}
	h.renderPreview(c, document, page.ProjectID, page.DraftPath, true, workbenchservice.PreviewDocPage)
}

func (h *Handle) renderPreview(c *gin.Context, document json.RawMessage, projectID, currentPath string,
	withEditorBridge bool, kind workbenchservice.PreviewDocKind) {

	html, outcome, err := h.svc.RenderPreview(c.Request.Context(), document, projectID, currentPath,
		c.Query("lang"), withEditorBridge)
	switch outcome {
	case workbenchservice.PreviewOK:
		if withEditorBridge {
			html = []byte(injectEditorBridge(string(html), canvasBridgeTr(c)))
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", html)
	case workbenchservice.PreviewInvalidDocument:
		response.ErrorWithMessage(c, http.StatusBadRequest, workbenchenums.ErrDraftDecodeFailed)
	case workbenchservice.PreviewCompileRejected:
		writePreviewCompileRejected(c, document, kind, err)
	default:

		response.ErrorWithMessage(c, http.StatusInternalServerError, response.InternalKey)
	}
}

// canvasBridgeTr 是注入给编辑器桥脚本的取词闭包。
//
// 桥脚本把菜单项文案嵌进 HTML 字符串（物理上不经过 Jet 模板），这是已登记的例外；
// 但取词本身仍走 pkg/response 的正式口，不直连 i18n。
func canvasBridgeTr(c *gin.Context) func(key, fallback string) string {
	return func(key, fallback string) string {
		return response.MsgText(c, facing.Msg{Key: key, Fallback: fallback})
	}
}

//go:embed scripts/editor_bridge.js
var editorBridgeJSSource string

func editorBridgeScriptFor(tr func(key, fallback string) string) string {
	pairs := make([]string, 0, len(editorBridgeTexts)*2)
	for _, it := range editorBridgeTexts {
		pairs = append(pairs, "{{"+it.Name+"}}", jsSingleQuoted(tr(it.Key, it.Fallback)))
	}
	body := strings.NewReplacer(pairs...).Replace(editorBridgeJSSource)

	return body
}

var editorBridgeTexts = []struct{ Name, Key, Fallback string }{
	{"bridge.insert", workbenchenums.BridgeInsert, "插入组件"},
	{"bridge.editText", workbenchenums.BridgeEditText, "编辑文本"},
	{"bridge.copy", workbenchenums.BridgeCopy, "复制"},
	{"bridge.cut", workbenchenums.BridgeCut, "剪切"},
	{"bridge.pasteInside", workbenchenums.BridgePasteInside, "粘贴到内部"},
	{"bridge.moveUp", workbenchenums.BridgeMoveUp, "上移"},
	{"bridge.moveDown", workbenchenums.BridgeMoveDown, "下移"},
	{"bridge.delete", workbenchenums.BridgeDelete, "删除"},
	{"bridge.entranceGroup", workbenchenums.BridgeEntranceGroup, "入场动画"},
	{"bridge.hoverLift", workbenchenums.BridgeHoverLift, "悬浮上浮"},
}

func jsSingleQuoted(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		"\r", `\r`,
		"\n", `\n`,
		"</", `<\/`,
	).Replace(s)
}

func editorBridgeScriptTag(body string) string {
	return "<script>" + body + "</script>"
}

func injectEditorBridge(html string, tr func(key, fallback string) string) string {
	idx := strings.LastIndex(html, "</body>")
	if idx < 0 {
		return html
	}

	return html[:idx] + editorBridgeScriptTag(editorBridgeScriptFor(tr)) + html[idx:]
}

const (
	EditTargetPage     = "page"
	EditTargetBlock    = "block"
	EditTargetTemplate = "template"

	EditTargetInstance = "instance"
)

type TargetEndpoint struct {
	Path string `json:"path"`
}

type TargetSaveBody struct {
	IDKey string `json:"idKey"`

	DocumentKey string `json:"documentKey"`

	VersionKey string `json:"versionKey,omitempty"`

	PathKey string `json:"pathKey,omitempty"`

	Extras map[string]string `json:"extras,omitempty"`
}

type TargetCapabilities struct {
	Publish  bool `json:"publish"`
	Build    bool `json:"build"`
	Rollback bool `json:"rollback"`
	History  bool `json:"history"`
	URL      bool `json:"url"`
}

type EditTarget struct {
	Type     string             `json:"type"`
	Save     TargetEndpoint     `json:"save"`
	SaveBody TargetSaveBody     `json:"saveBody"`
	Caps     TargetCapabilities `json:"caps"`
}

var workbenchTargets = map[string]EditTarget{
	EditTargetPage: {
		Type: EditTargetPage,
		Save: TargetEndpoint{Path: "/api/page/draft/save"},
		SaveBody: TargetSaveBody{
			IDKey: "id", DocumentKey: "draftDocument",
			VersionKey: "expectedVersion", PathKey: "draftPath",
		},

		Caps: TargetCapabilities{Publish: true, Build: true, Rollback: true, History: true, URL: true},
	},
	EditTargetBlock: {
		Type: EditTargetBlock,
		Save: TargetEndpoint{Path: "/admin/blocks/save-content"},
		SaveBody: TargetSaveBody{
			IDKey: "id", DocumentKey: "document", VersionKey: "expectedVersion",
			Extras: map[string]string{"name": "blockName"},
		},

		Caps: TargetCapabilities{},
	},
	EditTargetTemplate: {
		Type:     EditTargetTemplate,
		Save:     TargetEndpoint{Path: "/api/contenttemplate/update"},
		SaveBody: TargetSaveBody{IDKey: "id", DocumentKey: "draftDocument"},

		Caps: TargetCapabilities{},
	},

	EditTargetInstance: {
		Type: EditTargetInstance,
		Save: TargetEndpoint{Path: "/workbench/instance/save"},
		SaveBody: TargetSaveBody{
			IDKey: "id", DocumentKey: "draftDocument",
			Extras: map[string]string{"projectId": "projectId"},
		},
		Caps: TargetCapabilities{},
	},
}

func workbenchTargetOf(targetType string) EditTarget {
	target, ok := EditTargetFor(targetType)
	if !ok {
		panic("工作台编辑目标未注册: " + targetType)
	}
	return target
}

func EditTargetFor(targetType string) (EditTarget, bool) {
	t, ok := workbenchTargets[targetType]
	return t, ok
}

func WorkbenchTargets() []EditTarget {
	types := make([]string, 0, len(workbenchTargets))
	for t := range workbenchTargets {
		types = append(types, t)
	}
	sort.Strings(types)
	out := make([]EditTarget, 0, len(types))
	for _, t := range types {
		out = append(out, workbenchTargets[t])
	}
	return out
}

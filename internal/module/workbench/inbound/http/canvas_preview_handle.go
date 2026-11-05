package workbenchhttp

import (
	"go_wp/internal/builder/extensions/presets"
	_ "embed"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/pkg/builder"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	workbenchdto "go_wp/internal/module/workbench/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/upload"
)

func (h *Handle) TemplatePreview(c *gin.Context) {
	if instanceID := strings.TrimSpace(c.Query("instance")); instanceID != "" {
		h.renderInstancePreview(c, instanceID, c.Query("projectId"), nil, c.Query("editor") == "1")
		return
	}
	templateID := strings.TrimSpace(c.Query("template"))
	if templateID == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, workbenchenums.ErrTemplateParamRequired)
		return
	}
	projectID := c.Query("projectId")
	tpl, ok := h.previewTemplateTarget(c, templateID, projectID)
	if !ok {
		return
	}
	if contenttemplatecontract.IsStructureTemplateType(tpl.EntityType) {

		h.renderStructureTemplatePreview(c, tpl.DraftDocument, projectID, c.Query("editor") == "1")
		return
	}
	if h.templatePreview == nil {
		response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.ErrContentTemplatePreviewNotAssembled)
		return
	}
	entityType := strings.TrimSpace(c.Query("entityType"))
	entityID := strings.TrimSpace(c.Query("entityId"))
	if entityType == "" || entityID == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, workbenchenums.ErrPreviewParamsRequired)
		return
	}
	h.renderTemplatePreview(c, templateID, entityType, entityID, projectID, nil,
		c.Query("editor") == "1")
}

func (h *Handle) TemplatePreviewDraft(c *gin.Context) {
	req, _ := binding.ParseForm[workbenchdto.TemplatePreviewDraftReq](c)
	templateID := strings.TrimSpace(req.ID)
	document := json.RawMessage(req.DraftDocument)
	if req.InstanceID != "" && len(document) > 0 {
		h.renderInstancePreview(c, req.InstanceID, req.ProjectID, document, true)
		return
	}
	if templateID == "" || len(document) == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, workbenchenums.ErrPreviewParamsIncomplete)
		return
	}
	projectID := req.ProjectID
	tpl, ok := h.previewTemplateTarget(c, templateID, projectID)
	if !ok {
		return
	}
	if contenttemplatecontract.IsStructureTemplateType(tpl.EntityType) {
		h.renderStructureTemplatePreview(c, document, projectID, true)
		return
	}
	if h.templatePreview == nil {
		response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.ErrContentTemplatePreviewNotAssembled)
		return
	}
	entityType := strings.TrimSpace(req.EntityType)
	entityID := strings.TrimSpace(req.EntityID)
	if entityType == "" || entityID == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, workbenchenums.ErrPreviewParamsIncomplete)
		return
	}
	h.renderTemplatePreview(c, templateID, entityType, entityID, projectID, document, true)
}

func (h *Handle) renderStructureTemplatePreview(c *gin.Context, document json.RawMessage,
	projectID string, withEditorBridge bool) {
	if h.pages == nil {
		response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.ErrStructureTemplatePreviewNotAssembled)
		return
	}
	if len(document) == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, workbenchenums.ErrTemplateDocumentEmpty)
		return
	}
	h.renderPreview(c, document, projectID, "", withEditorBridge, workbenchservice.PreviewDocStructureTemplate)
}

func (h *Handle) renderTemplatePreview(c *gin.Context, templateID, entityType, entityID, projectID string,
	draftDocument json.RawMessage, withEditorBridge bool) {
	h.renderEntityPreview(c, &presentationdto.PreviewInstanceReq{
		EntityType: entityType, EntityID: entityID, TemplateID: templateID,
		ProjectID: projectID, DraftDocument: draftDocument,
	}, withEditorBridge)
}

func (h *Handle) renderInstancePreview(c *gin.Context, instanceID, projectID string, document json.RawMessage, withEditorBridge bool) {
	h.renderEntityPreview(c, &presentationdto.PreviewInstanceReq{InstanceID: instanceID, ProjectID: projectID, DraftDocument: document}, withEditorBridge)
}

func (h *Handle) renderEntityPreview(c *gin.Context, req *presentationdto.PreviewInstanceReq, withEditorBridge bool) {
	if h.templatePreview == nil {
		response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.ErrContentTemplatePreviewNotAssembled)
		return
	}
	res, err := h.templatePreview.PreviewInstance(c.Request.Context(), req)
	if err != nil {

		logger.Scene("content_template").With("path", c.Request.URL.Path).Error(err, "模板编译失败")

		message := workbenchCompileFallbackText(c)
		if key, ok := workbenchservice.TemplatePreviewFacingKey(err.Error()); ok {
			if text := workbenchFacingText(c, key); text != "" {
				message = text
			}
		}
		response.ErrorWithMessage(c, http.StatusUnprocessableEntity, message)
		return
	}
	html := res.HTML
	if withEditorBridge {
		html = injectEditorBridge(html, i18n.TranslateFunc(response.RequestLanguage(c)))
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func (h *Handle) SetInstanceOverrideDeps(instances presentationcontract.PresentationService) {
	h.instances = instances
}

func (h *Handle) workbenchInstance(c *gin.Context, instanceID string) {
	if h.instances == nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).
			Warn("实例编辑能力未装配")
		response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.ErrInstanceEditNotAssembled)
		return
	}
	ctx := c.Request.Context()
	inst, err := h.instances.Get(ctx, &presentationdto.GetReq{ID: instanceID, ProjectID: c.Query("projectId")})
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("instanceId", instanceID).
			Error(err, "呈现实例读取失败")
		response.ErrorWithMessage(c, http.StatusNotFound, workbenchenums.ErrInstanceNotFound)
		return
	}

	document := inst.Document
	if len(document) == 0 {

		response.ErrorWithMessage(c, http.StatusUnprocessableEntity,
			workbenchFacingText(c, workbenchenums.ErrInstanceNoDocument))
		return
	}
	documentJSON, derr := json.Marshal(document)
	if derr != nil {

		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("instanceId", instanceID).
			Error(derr, "实例文档序列化失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrDraftEncodeFailed)
		return
	}

	renderMode := strings.TrimSpace(inst.RenderMode)
	if renderMode != presentationdto.RenderModeDocument {
		renderMode = presentationdto.RenderModeTemplate
	}
	renderModeLabel := workbenchShortText(c, workbenchenums.ModeFollowTemplate)
	if renderMode == presentationdto.RenderModeDocument {
		renderModeLabel = workbenchShortText(c, workbenchenums.ModeDocument)
	}
	metaJSON, merr := json.Marshal(gin.H{
		"mediaBaseUrl":     upload.BaseURL(),
		"businessPresets":  presets.BusinessPresets(),
		"componentActions": builder.ComponentActions(),
		"blocks":           h.svc.BlockSummaries(c.Request.Context(), inst.ProjectID),
		"target":           workbenchTargetOf(EditTargetInstance), "pageId": inst.ID,
		"saveBase": "instance", "entityType": inst.EntityType, "entityId": inst.EntityID,
		"projectId": inst.ProjectID, "draftPath": "", "instanceMode": true,
		"renderMode": renderMode,
	})
	if merr != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("instanceId", instanceID).
			Error(merr, "工作台编辑器元数据序列化失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)
		return
	}
	schemas, serr := componentsForCanvas(nil)
	if serr != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("instanceId", instanceID).
			Error(serr, "组件 schema 生成失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)
		return
	}
	schemasJSON, serr2 := json.Marshal(schemas)
	if serr2 != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("instanceId", instanceID).
			Error(serr2, "组件 schema 序列化失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)
		return
	}

	pageview.RenderTemplate(c, CanvasLayoutTemplate, &CanvasPageResp{
		Title:      workbenchShortText(c, workbenchenums.TitleInstance),
		PageID:     inst.ID,
		IsBlock:    false,
		IsTemplate: true,
		Document:   string(documentJSON),
		Meta:       string(metaJSON),
		Schemas:    string(schemasJSON),
		PreviewQS: "instance=" + inst.ID + "&entityType=" + inst.EntityType +
			"&entityId=" + inst.EntityID + "&editor=1&projectId=" + inst.ProjectID,
		JSVer: workbenchservice.StaticJSVersion(),

		InstanceMode:    true,
		RenderMode:      renderMode,
		RenderModeLabel: renderModeLabel,
	})
}

func (h *Handle) InstanceSave(c *gin.Context) {
	if h.instances == nil {

		response.ErrorWithMessage(c, http.StatusServiceUnavailable, adminui.MsgInternalError)
		return
	}
	body, err := binding.ParseJSON[presentationdto.WorkbenchSaveReq](c)
	if err != nil || body.Override().InstanceID == "" || len(body.DraftDocument) == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrSaveParamsIncomplete))
		return
	}
	req := body.Override()
	res, err := h.instances.SaveOverrideDocument(c.Request.Context(), req)
	if err != nil {

		if strings.TrimSpace(err.Error()) == presentationenums.ErrDetachConfirmRequired {
			response.ErrorWithMessage(c, http.StatusConflict, workbenchShortText(c, workbenchenums.ErrDetachConfirmRequired))
			return
		}

		logger.Scene("workbench").With("path", c.Request.URL.Path).Error(err, "实例文档保存失败")
		message := workbenchCompileFallbackText(c)
		if key, ok := workbenchservice.InstanceSaveFacingKey(err.Error()); ok {
			if text := workbenchFacingText(c, key); text != "" {
				message = text
			}
		}
		response.ErrorWithMessage(c, http.StatusUnprocessableEntity, message)
		return
	}
	mode := presentationdto.RenderModeTemplate
	if res != nil && res.RenderMode != "" {
		mode = res.RenderMode
	}

	response.SuccessWithMessage(c, "", gin.H{"renderMode": mode})
}

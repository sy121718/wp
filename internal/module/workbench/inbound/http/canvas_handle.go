package workbenchhttp

import (
	"go_wp/internal/builder/extensions/presets"
	_ "embed"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/builder"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	plugincontract "go_wp/internal/module/plugin/contract"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/safepath"
	"go_wp/pkg/upload"
	"go_wp/pkg/utils"
)

func (h *Handle) Workbench(c *gin.Context) {
	if blockID := strings.TrimSpace(c.Query("block")); blockID != "" {
		h.workbenchBlock(c, blockID)
		return
	}
	if templateID := strings.TrimSpace(c.Query("template")); templateID != "" {
		h.workbenchTemplate(c, templateID)
		return
	}
	if instanceID := strings.TrimSpace(c.Query("instance")); instanceID != "" {
		h.workbenchInstance(c, instanceID)
		return
	}
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
	documentJSON, err := json.Marshal(page.DraftDocument)
	if err != nil {

		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).Error(err, "草稿文档序列化失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrDraftEncodeFailed)
		return
	}

	var pluginComponents []plugincontract.ComponentSummary
	var pluginPresets []plugincontract.PresetSummary
	asm := h.svc.PluginAssembly(c.Request.Context())
	if asm != nil {
		pluginComponents = asm.Components
		pluginPresets = asm.Presets
	}

	if pluginPresets == nil {
		pluginPresets = []plugincontract.PresetSummary{}
	}
	metaJSON, err := json.Marshal(gin.H{

		"mediaBaseUrl":     upload.BaseURL(),
		"businessPresets":  presets.BusinessPresets(),
		"componentActions": builder.ComponentActions(),
		"target":           workbenchTargetOf(EditTargetPage),
		"pageId":           page.ID,
		"projectId":        page.ProjectID,
		"draftPath":        page.DraftPath,
		"version":          page.DraftVersion,

		"blocks": h.svc.BlockSummaries(c.Request.Context(), page.ProjectID),

		"themeId":       workbenchservice.ThemeIDOf(page),
		"themeSettings": h.svc.ThemeSettingsOf(c.Request.Context(), page),

		"plugins": pluginComponents,

		"presets": pluginPresets,
	})
	if err != nil {

		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).Error(err, "工作台编辑器元数据序列化失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)
		return
	}

	schemas, err := componentsForCanvas(asm)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).Error(err, "组件 schema 生成失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).Error(err, "组件 schema 序列化失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)
		return
	}
	pageview.RenderTemplate(c, CanvasLayoutTemplate, &CanvasPageResp{
		Title:     workbenchservice.WorkbenchTitle(page, workbenchTrFunc(c)),
		PageID:    page.ID,
		IsBlock:   false,
		DraftPath: page.DraftPath,
		Version:   page.DraftVersion,
		Document:  utils.JsonSafe(string(documentJSON)),
		Meta:      utils.JsonSafe(string(metaJSON)),
		Schemas:   utils.JsonSafe(string(schemasJSON)),
		JSVer:     workbenchservice.StaticJSVersion(),
	})
}

func (h *Handle) workbenchBlock(c *gin.Context, blockID string) {
	block, err := h.svc.BlockByID(c.Request.Context(), blockID)
	if err != nil || block == nil {

		if err != nil {
			logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("blockId", blockID).
				Error(err, "全局块读取失败")
		}
		response.ErrorWithMessage(c, http.StatusNotFound, workbenchenums.ErrBlockNotFound)
		return
	}
	documentJSON, err := json.Marshal(block.Document)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("blockId", blockID).
			Error(err, "块文档序列化失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrBlockEncodeFailed)
		return
	}
	meta := gin.H{

		"mediaBaseUrl":     upload.BaseURL(),
		"businessPresets":  presets.BusinessPresets(),
		"componentActions": builder.ComponentActions(),
		"target":           workbenchTargetOf(EditTargetBlock),
		"pageId":           block.ID,
		"projectId":        block.ProjectID,
		"saveBase":         "block",
		"blockName":        block.Name,
		"kind":             block.Kind,
		"blocks":           h.svc.BlockSummaries(c.Request.Context(), block.ProjectID),
		"draftPath":        "",
		"version":          block.Version,
	}

	if back := safepath.LocalReturnPath(c.Query("returnUrl")); back != "" {
		meta["returnUrl"] = back
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("blockId", blockID).
			Error(err, "工作台编辑器元数据序列化失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)
		return
	}
	schemas, err := componentsForCanvas(nil)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("blockId", blockID).
			Error(err, "组件 schema 生成失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("blockId", blockID).
			Error(err, "组件 schema 序列化失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)
		return
	}
	pageview.RenderTemplate(c, CanvasLayoutTemplate, &CanvasPageResp{
		Title:    workbenchShortText(c, workbenchenums.TitleBlockPrefix) + block.Name,
		PageID:   block.ID,
		IsBlock:  true,
		Version:  block.Version,
		Document: utils.JsonSafe(string(documentJSON)),
		Meta:     utils.JsonSafe(string(metaJSON)),
		Schemas:  utils.JsonSafe(string(schemasJSON)),
		JSVer:    workbenchservice.StaticJSVersion(),
	})
}

func (h *Handle) workbenchTemplate(c *gin.Context, templateID string) {
	if !h.svc.ContentTemplatesReady() {

		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).
			Warn("内容模板编辑能力未装配")
		response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.ErrContentTemplateEditNotAssembled)
		return
	}

	tpl, err := h.svc.TemplateByID(c.Request.Context(), templateID, "")
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("templateId", templateID).
			Error(err, "内容模板读取失败")
		response.ErrorWithMessage(c, http.StatusNotFound, workbenchenums.ErrTemplateNotFound)
		return
	}

	noEntity := contenttemplatecontract.IsStructureTemplateType(tpl.EntityType)
	entityID := strings.TrimSpace(c.Query("entityId"))
	entityType := strings.TrimSpace(c.Query("entityType"))
	if entityType == "" || noEntity {

		entityType = tpl.EntityType
	}
	if noEntity {

		entityID = ""
	} else if entityID == "" {

		response.ErrorWithMessage(c, http.StatusBadRequest,
			strings.ReplaceAll(workbenchShortText(c, workbenchenums.ErrPreviewEntityIDRequired), "{type}", entityType))
		return
	}

	if !noEntity && h.templatePreview == nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("mode", "template-preview").
			Warn("模板预览能力未装配")
		response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.ErrContentTemplateEditNotAssembled)
		return
	}
	if noEntity && h.pages == nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("mode", "structure-preview").
			Warn("无实体模板预览能力未装配")
		response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.ErrStructureTemplatePreviewNotAssembled)
		return
	}
	projectID := strings.TrimSpace(c.Query("projectId"))
	documentJSON, err := json.Marshal(tpl.DraftDocument)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("templateId", templateID).
			Error(err, "模板文档序列化失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrTemplateEncodeFailed)
		return
	}
	metaJSON, err := json.Marshal(gin.H{

		"mediaBaseUrl":     upload.BaseURL(),
		"businessPresets":  presets.BusinessPresets(),
		"componentActions": builder.ComponentActions(),
		"target":           workbenchTargetOf(EditTargetTemplate),
		"pageId":           tpl.ID,
		"saveBase":         "template",
		"templateName":     tpl.Name,
		"entityType":       entityType,
		"entityId":         entityID,
		"noEntity":         noEntity,
		"blocks":           h.svc.BlockSummaries(c.Request.Context(), projectID),
		"projectId":        projectID,
		"draftPath":        "",
		"version":          tpl.DraftVersion,
	})
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("templateId", templateID).
			Error(err, "工作台编辑器元数据序列化失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)
		return
	}
	schemas, err := componentsForCanvas(nil)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("templateId", templateID).
			Error(err, "组件 schema 生成失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("templateId", templateID).
			Error(err, "组件 schema 序列化失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)
		return
	}
	previewQS := workbenchservice.TemplatePreviewQuery(tpl.ID, entityType, entityID, projectID)
	pageview.RenderTemplate(c, CanvasLayoutTemplate, &CanvasPageResp{
		Title:      workbenchShortText(c, workbenchenums.TitleTemplatePrefix) + tpl.Name,
		PageID:     tpl.ID,
		IsBlock:    false,
		IsTemplate: true,

		IsStructureTemplate: noEntity,
		Document:            utils.JsonSafe(string(documentJSON)),
		Meta:                utils.JsonSafe(string(metaJSON)),
		Schemas:             utils.JsonSafe(string(schemasJSON)),
		PreviewQS:           previewQS,
		JSVer:               workbenchservice.StaticJSVersion(),
	})
}

func (h *Handle) previewTemplateTarget(c *gin.Context, templateID, projectID string) (tpl *contenttemplatedto.TemplateResp, ok bool) {
	if !h.svc.ContentTemplatesReady() {
		response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.ErrContentTemplateEditNotAssembled)
		return nil, false
	}
	tpl, err := h.svc.TemplateByID(c.Request.Context(), templateID, projectID)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, workbenchenums.ErrTemplateNotFound)
		return nil, false
	}
	return tpl, true
}

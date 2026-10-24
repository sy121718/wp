package workbenchhttp

import (
	_ "embed"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
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
)

// TemplatePreview 基于已保存模板 + 样例实体编译预览（画布 iframe GET）。
//
// 结构模板（页眉 / 页脚）没有样例实体：entityId 缺省/为空时按无实体模式渲染
// （判据是**模板自己的 entity_type**，见 workbenchTemplate 的同名说明）。
func (h *Handle) TemplatePreview(c *gin.Context) {
	templateID := strings.TrimSpace(c.Query("template"))
	if templateID == "" {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrTemplateParamRequired))
		return
	}
	projectID := c.Query("projectId")
	tpl, ok := h.previewTemplateTarget(c, templateID, projectID)
	if !ok {
		return
	}
	if contenttemplatecontract.IsStructureTemplateType(tpl.EntityType) {
		// 无实体模式：结构模板的 entityId 一律忽略（它不是内容实体，没有字段来源）。
		h.renderStructureTemplatePreview(c, tpl.DraftDocument, projectID, c.Query("editor") == "1")
		return
	}
	if h.templatePreview == nil {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrContentTemplatePreviewNotAssembled))
		return
	}
	entityType := strings.TrimSpace(c.Query("entityType"))
	entityID := strings.TrimSpace(c.Query("entityId"))
	if entityType == "" || entityID == "" {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrPreviewParamsRequired))
		return
	}
	h.renderTemplatePreview(c, templateID, entityType, entityID, projectID, nil,
		c.Query("editor") == "1")
}

// TemplatePreviewDraft 基于未保存 AST + 样例实体返回临时预览（POST，画布刷新）。
// 结构模板同 TemplatePreview：走无实体模式，草稿文档直接编译。
func (h *Handle) TemplatePreviewDraft(c *gin.Context) {
	req, _ := binding.ParseForm[workbenchdto.TemplatePreviewDraftReq](c)
	templateID := strings.TrimSpace(req.ID)
	document := json.RawMessage(req.DraftDocument)
	if templateID == "" || len(document) == 0 {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrPreviewParamsIncomplete))
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
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrContentTemplatePreviewNotAssembled))
		return
	}
	entityType := strings.TrimSpace(req.EntityType)
	entityID := strings.TrimSpace(req.EntityID)
	if entityType == "" || entityID == "" {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrPreviewParamsIncomplete))
		return
	}
	h.renderTemplatePreview(c, templateID, entityType, entityID, projectID, document, true)
}

// renderStructureTemplatePreview 无样例实体模式下的模板预览（结构模板：页眉 / 页脚）。
//
// 为什么走 page 编译管线（CompilePreview，与页面 / 全局块画布同一入口）而不是
// presentation.PreviewInstance —— 这个选择决定将来别人给结构模板加能力时的走向：
//
//  1. PreviewInstance 整条链是**以实体为前提**的：ValidateFieldRefs 按 entityType 校验、
//     registry.ResolverFor(entityType, entityID) 取实体解析器、applyEntitySEO 读实体字段。
//     在那条链上开一个「跳过」分支，等于让后续任何一处新增的实体依赖在无实体模式下静默
//     落空（页眉渲染成空），而结构模板**根本不是实体实例**，不该被塞进实体实例的路径。
//  2. 页面编译管线本身就**没有实体解析器**（Page Document 从不含字段绑定），它的 Compile
//     选项正是结构模板在正式构建里所用的那一份：同一 builder.Compile、同一组件集 / 插件
//     装配 / 主题 / 站点级选项 / 结构槽位展开 / 内容翻译 / ClientAsset。正式构建里结构模板
//     也是以 root 节点进入同一个 builder.Compile 的（pipeline.BuildStructureSlots）。
//  3. 字段绑定**跳过而不是伪造**：这里不注入、也不伪造实体解析器。真出现绑定（旧数据 /
//     绕过保存期校验的写入），编译器会按「解析器未注入」直接报错 —— 可见的失败，而不是
//     渲染成一片空白的假通过。结构模板不许有绑定这条不变量因此没有被削弱。
//
// currentPath 传空：结构模板文档没有自己的访问路径（它只作为槽位出现在引用页里）。
func (h *Handle) renderStructureTemplatePreview(c *gin.Context, document json.RawMessage,
	projectID string, withEditorBridge bool) {
	if h.pages == nil {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrStructureTemplatePreviewNotAssembled))
		return
	}
	if len(document) == 0 {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrTemplateDocumentEmpty))
		return
	}
	h.renderPreview(c, document, projectID, "", withEditorBridge, workbenchservice.PreviewDocStructureTemplate)
}

func (h *Handle) renderTemplatePreview(c *gin.Context, templateID, entityType, entityID, projectID string,
	draftDocument json.RawMessage, withEditorBridge bool) {
	res, err := h.templatePreview.PreviewInstance(c.Request.Context(), &presentationdto.PreviewInstanceReq{
		EntityType: entityType, EntityID: entityID, TemplateID: templateID,
		ProjectID: projectID, DraftDocument: draftDocument,
	})
	if err != nil {
		// 422 保留（编译失败对作者是业务信息），错误原文只进日志：
		// 编译器的错误里带节点路径与模板片段，直接铺在页面上等于把内部结构公开。
		logger.Scene("content_template").With("path", c.Request.URL.Path).Error(err, "模板编译失败")
		// 可归因的几类（模板类型串用 / 字段绑定越界 / 工程作用域没定下来）给能照着做的文案，
		// 其余归口：分类判据见 service.TemplatePreviewFacingKey。
		if key, ok := workbenchservice.TemplatePreviewFacingKey(err.Error()); ok {
			if text := workbenchFacingText(c, key); text != "" {
				c.String(http.StatusUnprocessableEntity, text)
				return
			}
		}
		c.String(http.StatusUnprocessableEntity, workbenchCompileFallbackText(c))
		return
	}
	html := res.HTML
	if withEditorBridge {
		html = injectEditorBridge(html, i18n.TranslateFunc(response.RequestLanguage(c)))
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

// SetInstanceOverrideDeps 注入实例编辑模式端口（装配期调用；未注入时页面明确提示）。
func (h *Handle) SetInstanceOverrideDeps(instances presentationcontract.PresentationService) {
	h.instances = instances
}

// workbenchInstance 实例编辑模式外壳。
func (h *Handle) workbenchInstance(c *gin.Context, instanceID string) {
	if h.instances == nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).
			Warn("实例编辑能力未装配")
		renderWorkbenchPageError(c, http.StatusServiceUnavailable, workbenchenums.ErrInstanceEditNotAssembled)
		return
	}
	ctx := c.Request.Context()
	inst, err := h.instances.Get(ctx, &presentationdto.GetReq{ID: instanceID, ProjectID: c.Query("projectId")})
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("instanceId", instanceID).
			Error(err, "呈现实例读取失败")
		renderWorkbenchPageError(c, http.StatusNotFound, workbenchenums.ErrInstanceNotFound)
		return
	}
	// 生效文档：override 优先（toResp 随快照返回），否则实例尚无可编辑文档，
	// 由调用方（商品编辑页）先经 CreateInstance/Rebuild 产生快照再进来。
	document := inst.Document
	if len(document) == 0 {
		// 422 保留（这是「这个实例还不能编辑」的业务状态，不是服务端故障），
		// 文案走 key：硬编码中文在英文站点上不会翻译，且说不清下一步怎么做。
		renderWorkbenchPageErrorText(c, http.StatusUnprocessableEntity, workbenchenums.ErrInstanceNoDocument,
			workbenchFacingText(c, workbenchenums.ErrInstanceNoDocument))
		return
	}
	documentJSON, derr := json.Marshal(document)
	if derr != nil {
		// 原先这里的序列化错误被 `_` 丢掉：页面上是一张白屏，日志里什么都没有。
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("instanceId", instanceID).
			Error(derr, "实例文档序列化失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrDraftEncodeFailed)
		return
	}
	// 渲染模式（迁移 282，双轨）：状态条据此显示「跟随模板中 / 独立文档」——
	// 编辑者必须随时知道自己在改的是共享模板还是这一个商品，否则会以为
	// 改模板会影响全站（或反之）。
	renderMode := strings.TrimSpace(inst.RenderMode)
	if renderMode != presentationdto.RenderModeDocument {
		renderMode = presentationdto.RenderModeTemplate
	}
	renderModeLabel := workbenchShortText(c, workbenchenums.ModeFollowTemplate)
	if renderMode == presentationdto.RenderModeDocument {
		renderModeLabel = workbenchShortText(c, workbenchenums.ModeDocument)
	}
	metaJSON, merr := json.Marshal(gin.H{
		"target": workbenchTargetOf(EditTargetInstance), "pageId": inst.ID,
		"saveBase": "instance", "entityType": inst.EntityType, "entityId": inst.EntityID,
		"projectId": inst.ProjectID, "draftPath": "", "instanceMode": true,
		"renderMode": renderMode,
	})
	if merr != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("instanceId", instanceID).
			Error(merr, "工作台编辑器元数据序列化失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)
		return
	}
	schemas, serr := componentsForCanvas(nil)
	if serr != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("instanceId", instanceID).
			Error(serr, "组件 schema 生成失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)
		return
	}
	schemasJSON, serr2 := json.Marshal(schemas)
	if serr2 != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("instanceId", instanceID).
			Error(serr2, "组件 schema 序列化失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)
		return
	}
	// 与另外三种画布模式一致地走 adminui.Prepare：它注入 t（画布模板的取词函数）、
	// csrf_token（workbench.js 的 POST fetch 读它）与 lang。此前这个分支直接传 gin.H，
	// 于是 layout.html 的 `{{ .["csrf_token"] }}` 恒为空串 —— JS 只能靠 sessionStorage 兜底。
	// 与另外三种模式**同一个视图类型**（CanvasPageResp）：同一张模板用两套键集，
	// 加一个键只改一处的约定就守不住了 —— 实例模式曾经就因为漏给 jsVer，
	// 整页在 <head> 第 17 行中断、只回 445 字节。
	c.HTML(http.StatusOK, "workbench/layout", adminui.PrepareView(c, &workbenchdto.CanvasPageResp{
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
		// 双轨状态条（layout.html 据 isset(.instanceMode) 渲染）
		InstanceMode:    true,
		RenderMode:      renderMode,
		RenderModeLabel: renderModeLabel,
	}))
}

// InstanceSave POST /workbench/instance/save：保存实例覆盖文档并重建发布。
// 只改本实例（override_document + 重编译发布），不影响共享模板（docs/04-C）。
func (h *Handle) InstanceSave(c *gin.Context) {
	if h.instances == nil {
		// 形态与同文件其余分支一致（c.JSON + code/message）：本端点由前端 fetch 消费，
		// api.js 的 send() 直接 r.json()，正文是 text/plain 时解析抛异常 → then 链断掉 →
		// 到不了那句 alert，用户看不到任何提示。文案走归口译文：能力未装配属装配缺陷，
		// 给用户看的只能是受控提示，装配细节不进响应。
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": adminui.PageInternalText(c)})
		return
	}
	body, err := binding.ParseJSON[presentationdto.WorkbenchSaveReq](c)
	if err != nil || body.Override().InstanceID == "" || len(body.DraftDocument) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": workbenchShortText(c, workbenchenums.ErrSaveParamsIncomplete)})
		return
	}
	req := body.Override()
	res, err := h.instances.SaveOverrideDocument(c.Request.Context(), req)
	if err != nil {
		// 需要确认才能转入独立文档：回 409 + JSON，由前端弹确认后重试。
		// 顺序是「先请求、再确认」，因为判据（文档结构是否真的变了）只有服务端算得准。
		if strings.TrimSpace(err.Error()) == presentationenums.ErrDetachConfirmRequired {
			c.JSON(http.StatusConflict, gin.H{
				"code":    409,
				"message": workbenchShortText(c, workbenchenums.ErrDetachConfirmRequired),
			})
			return
		}
		// 其余失败：编译/校验原文带节点路径与模板片段，只进日志。
		// 能归因的（编译失败 / 工程作用域没定下来）给一条说清「没写入」与「怎么修」的文案，
		// 归不了因的给归口文案 —— 判据见 service.InstanceSaveFacingKey。
		logger.Scene("workbench").With("path", c.Request.URL.Path).Error(err, "实例文档保存失败")
		message := workbenchCompileFallbackText(c)
		if key, ok := workbenchservice.InstanceSaveFacingKey(err.Error()); ok {
			if text := workbenchFacingText(c, key); text != "" {
				message = text
			}
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": 422, "message": message})
		return
	}
	mode := presentationdto.RenderModeTemplate
	if res != nil && res.RenderMode != "" {
		mode = res.RenderMode
	}
	// data.renderMode 让前端保存成功后即时把状态条切成「独立文档」。
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "", "data": gin.H{"renderMode": mode}})
}

// workbench_preview_handle.go - 工作台预览渲染（页面 / 块草稿编译直出）。

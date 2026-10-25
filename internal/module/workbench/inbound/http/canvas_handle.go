package workbenchhttp

import (
	_ "embed"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	plugincontract "go_wp/internal/module/plugin/contract"
	workbenchdto "go_wp/internal/module/workbench/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/pkg/logger"
	"go_wp/pkg/safepath"
	"go_wp/pkg/utils"
)

// Workbench 可视化编辑器外壳：注入 Page 草稿 AST 与保存接口所需元数据。
// ?block=ID 进入全局块编辑模式（同一画布，保存走块接口、无发布链）。
// ?template=ID 进入内容模板编辑模式（保存走 contenttemplate.Update，预览需样例实体）。
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
		renderWorkbenchPageError(c, http.StatusBadRequest, workbenchenums.ErrMissingPageID)
		return
	}
	// 走统一出口 pageOf：Detail 的 projectID 是必填的越权防护 scope，只传 ID 会被
	// 契约层判为「参数缺失」，而这里把它显示成「页面不存在」—— 一个真实的 404 与
	// 一个漏传 scope 的调用，在页面上长得一模一样。
	page, err := h.svc.PageByID(c.Request.Context(), pageID)
	if err != nil {
		renderWorkbenchPageError(c, http.StatusNotFound, workbenchenums.ErrPageNotFound)
		return
	}
	documentJSON, err := json.Marshal(page.DraftDocument)
	if err != nil {
		// 未知内部异常：与 400/404 同壳但**状态码不同**（页面上也要能看出「不是你的参数
		// 问题」）。原文只进结构化日志 —— 序列化错误里带草稿结构片段，铺在页面上等于公开
		// 内部结构（同 workbench_err.go 的③结构化日志口径）。
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).Error(err, "草稿文档序列化失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrDraftEncodeFailed)
		return
	}
	// 启用插件的组件库摘要与区块预设（palette 注入，docs/06 §5/§5.2）。
	var pluginComponents []plugincontract.ComponentSummary
	var pluginPresets []plugincontract.PresetSummary
	asm := h.svc.PluginAssembly(c.Request.Context())
	if asm != nil {
		pluginComponents = asm.Components
		pluginPresets = asm.Presets
	}
	// 预设空时给空数组而非 nil（前端按数组读取，避免 undefined）。
	if pluginPresets == nil {
		pluginPresets = []plugincontract.PresetSummary{}
	}
	metaJSON, err := json.Marshal(gin.H{
		// target 编辑目标描述符（EDT-017）：前端按它决定保存端点与请求体键名，
		// 不认识任何一种目标类型 —— 新增目标时前端零改动。
		"target":    workbenchTargetOf(EditTargetPage),
		"pageId":    page.ID,
		"projectId": page.ProjectID,
		"draftPath": page.DraftPath,
		"version":   page.DraftVersion,
		// 全局块引用（core.globalref）候选：本工程全部块（组件库「全局块」分组）。
		"blocks": h.svc.BlockSummaries(c.Request.Context(), page.ProjectID),
		// 全局设置面板：页面挂接的主题与当前设置（颜色/字体），可就地修改保存。
		"themeId":       workbenchservice.ThemeIDOf(page),
		"themeSettings": h.svc.ThemeSettingsOf(c.Request.Context(), page),
		// 启用插件组件（组件库「插件组件」分组，type/label/hint/初始 props）。
		"plugins": pluginComponents,
		// 启用插件区块预设（组件库「区块预设」分组，id/label/category/thumbnail/document）。
		"presets": pluginPresets,
	})
	if err != nil {
		// 未知内部异常：路径与原文只进日志，页面上只给归口短句（与草稿序列化失败同口径）。
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).Error(err, "工作台编辑器元数据序列化失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)
		return
	}
	// 组件 Inspector 面板 schema（docs/02-C3）：声明式 Controls 驱动检查器表单，
	// 前端按 content/style/advanced 分组渲染，替代硬编码字段。
	// 插件组件 schema 与内置同构（docs/06 §5：上传即出现在检查器），在**自有副本**上合并 ——
	// 快照是只读的，插件 schema 不进程级缓存（否则插件禁用后 schema 残留）。
	schemas, err := componentsForCanvas(asm)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).Error(err, "组件 schema 生成失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).Error(err, "组件 schema 序列化失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)
		return
	}
	pageview.Render(c, &workbenchdto.CanvasPageResp{
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

// workbenchBlock 全局块编辑模式：复用工作台画布与检查器，
// 保存走 /api/block/update（无发布链、无 URL），meta.saveBase 指示前端切换接口前缀。
func (h *Handle) workbenchBlock(c *gin.Context, blockID string) {
	block, err := h.svc.BlockByID(c.Request.Context(), blockID)
	if err != nil || block == nil {
		// 404 是业务事实（块不存在或已删除）；读块时的故障也不在页面上分流，
		// 但必须留一行日志 —— 否则「数据库连不上」在页面上与「块被删了」长得一样。
		if err != nil {
			logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("blockId", blockID).
				Error(err, "全局块读取失败")
		}
		renderWorkbenchPageError(c, http.StatusNotFound, workbenchenums.ErrBlockNotFound)
		return
	}
	documentJSON, err := json.Marshal(block.Document)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("blockId", blockID).
			Error(err, "块文档序列化失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrBlockEncodeFailed)
		return
	}
	meta := gin.H{
		// target 编辑目标描述符（EDT-017）。
		"target":    workbenchTargetOf(EditTargetBlock),
		"pageId":    block.ID, // 复用键名：前端保存逻辑按 saveBase 切换接口
		"projectId": block.ProjectID,
		"saveBase":  "block",
		"blockName": block.Name,
		"kind":      block.Kind,
		"draftPath": "",
		"version":   0,
	}
	// returnUrl：菜单页「新建面板块并编辑」一路带过来的回跳目标。
	//
	// 白名单收敛在这一处（pkg/safepath.LocalReturnPath，task-75 自 shell 迁入）：不是站内
	// 相对路径的值直接**丢弃** —— meta 里没有这个键，保存后留在工作台，与普通块编辑完全一样。
	// 这里不做兜底跳转，因为「没带 returnUrl」与「带了非法 returnUrl」对用户是同一件事：
	// 不该离开编辑器。消费侧（SaveBlockContent）会再校验一次：meta 只是搬运，不是信任边界。
	if back := safepath.LocalReturnPath(c.Query("returnUrl")); back != "" {
		meta["returnUrl"] = back
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("blockId", blockID).
			Error(err, "工作台编辑器元数据序列化失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)
		return
	}
	schemas, err := componentsForCanvas(nil)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("blockId", blockID).
			Error(err, "组件 schema 生成失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("blockId", blockID).
			Error(err, "组件 schema 序列化失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)
		return
	}
	pageview.Render(c, &workbenchdto.CanvasPageResp{
		Title:    workbenchShortText(c, workbenchenums.TitleBlockPrefix) + block.Name,
		PageID:   block.ID,
		IsBlock:  true,
		Document: utils.JsonSafe(string(documentJSON)),
		Meta:     utils.JsonSafe(string(metaJSON)),
		Schemas:  utils.JsonSafe(string(schemasJSON)),
		JSVer:    workbenchservice.StaticJSVersion(),
	})
}

// workbenchTemplate 内容模板编辑模式（EDT-001）：复用工作台画布与检查器，
// 保存走 /api/contenttemplate/update。
//
// 两种预览模式（判据是**模板自己的 entity_type**，不是请求参数）：
//   - 结构模板（页眉 / 页脚）→ **无样例实体模式**：它们不是内容实体、也不接受字段
//     绑定（服务端 validateDocumentMode 对结构类型直接拒绝绑定），所以预览不需要
//     样例实体，entityId 传了也一律忽略；
//   - 内容实体模板（product / article / …）→ 仍**必须**有样例实体：字段绑定要按一条
//     真实记录解析，缺它只能看到空白组件（这正是这条校验存在的理由）。
func (h *Handle) workbenchTemplate(c *gin.Context, templateID string) {
	if !h.svc.ContentTemplatesReady() {
		// 503 不带异常对象：这是装配缺口（进程起来时没接上内容模板端口），
		// 一行 Warn 让「用户看到 503」在日志里有对应事实。
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).
			Warn("内容模板编辑能力未装配")
		renderWorkbenchPageError(c, http.StatusServiceUnavailable, workbenchenums.ErrContentTemplateEditNotAssembled)
		return
	}
	// projectID 留空：这里只按 id 取模板（工程作用域由预览入口 previewTemplateTarget 负责）。
	tpl, err := h.svc.TemplateByID(c.Request.Context(), templateID, "")
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("templateId", templateID).
			Error(err, "内容模板读取失败")
		renderWorkbenchPageError(c, http.StatusNotFound, workbenchenums.ErrTemplateNotFound)
		return
	}
	// 无实体模式按**模板行**判定：查询参数是请求方给的，拿它判等于让
	// 「product 模板 + entityType=header」把无实体模式开给普通模板（样例实体校验被绕过）。
	noEntity := contenttemplatecontract.IsStructureTemplateType(tpl.EntityType)
	entityID := strings.TrimSpace(c.Query("entityId"))
	entityType := strings.TrimSpace(c.Query("entityType"))
	if entityType == "" || noEntity {
		// 结构模板的类型以模板行为准：伪造的 entityType 不能把预览引到实体解析那条路。
		entityType = tpl.EntityType
	}
	if noEntity {
		// 结构模板没有实体来源：请求带来的 entityId 一律丢弃（无实体模式不解析字段绑定）。
		entityID = ""
	} else if entityID == "" {
		// 占位符 {type} 由词条携带：译文顺序可能与中文不同，所以不在这里拼进句子 ——
		// 取词交给渲染出口，替换只对**取到的译文**做。
		renderWorkbenchPageErrorText(c, http.StatusBadRequest, workbenchenums.ErrPreviewEntityIDRequired,
			strings.ReplaceAll(workbenchShortText(c, workbenchenums.ErrPreviewEntityIDRequired), "{type}", entityType))
		return
	}
	// 装配校验按模式分流：无实体模式走 page 编译管线，用不到模板预览端口。
	if !noEntity && h.templatePreview == nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("mode", "template-preview").
			Warn("模板预览能力未装配")
		renderWorkbenchPageError(c, http.StatusServiceUnavailable, workbenchenums.ErrContentTemplateEditNotAssembled)
		return
	}
	if noEntity && h.pages == nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("mode", "structure-preview").
			Warn("无实体模板预览能力未装配")
		renderWorkbenchPageError(c, http.StatusServiceUnavailable, workbenchenums.ErrStructureTemplatePreviewNotAssembled)
		return
	}
	projectID := strings.TrimSpace(c.Query("projectId"))
	documentJSON, err := json.Marshal(tpl.DraftDocument)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("templateId", templateID).
			Error(err, "模板文档序列化失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrTemplateEncodeFailed)
		return
	}
	metaJSON, err := json.Marshal(gin.H{
		// target 编辑目标描述符（EDT-017）。
		"target":       workbenchTargetOf(EditTargetTemplate),
		"pageId":       tpl.ID,
		"saveBase":     "template",
		"templateName": tpl.Name,
		"entityType":   entityType,
		"entityId":     entityID,
		"noEntity":     noEntity,
		"projectId":    projectID,
		"draftPath":    "",
		"version":      tpl.DraftVersion,
	})
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("templateId", templateID).
			Error(err, "工作台编辑器元数据序列化失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)
		return
	}
	schemas, err := componentsForCanvas(nil)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("templateId", templateID).
			Error(err, "组件 schema 生成失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		logger.Scene(workbenchErrScene).With("path", c.Request.URL.Path).With("templateId", templateID).
			Error(err, "组件 schema 序列化失败")
		renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)
		return
	}
	previewQS := workbenchservice.TemplatePreviewQuery(tpl.ID, entityType, entityID, projectID)
	pageview.Render(c, &workbenchdto.CanvasPageResp{
		Title:      workbenchShortText(c, workbenchenums.TitleTemplatePrefix) + tpl.Name,
		PageID:     tpl.ID,
		IsBlock:    false,
		IsTemplate: true,
		// 画布顶栏的模式提示：结构模板的预览不解析字段绑定，这件事要在编辑器里看得见
		//（否则作者会以为「页眉里该出现的商品名没出现」是渲染坏了）。
		IsStructureTemplate: noEntity,
		Document:            utils.JsonSafe(string(documentJSON)),
		Meta:                utils.JsonSafe(string(metaJSON)),
		Schemas:             utils.JsonSafe(string(schemasJSON)),
		PreviewQS:           previewQS,
		JSVer:               workbenchservice.StaticJSVersion(),
	})
}

// previewTemplateTarget 取预览目标模板；模板不存在时已写响应并返回 ok=false。
func (h *Handle) previewTemplateTarget(c *gin.Context, templateID, projectID string) (tpl *contenttemplatedto.TemplateResp, ok bool) {
	if !h.svc.ContentTemplatesReady() {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrContentTemplateEditNotAssembled))
		return nil, false
	}
	tpl, err := h.svc.TemplateByID(c.Request.Context(), templateID, projectID)
	if err != nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrTemplateNotFound))
		return nil, false
	}
	return tpl, true
}

package workbenchhttp

import (
	"encoding/json"
	"net/http"
	"strings"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"

	"go_wp/pkg/logger"

	"go_wp/internal/builder"

	"go_wp/internal/web/shell"

	"github.com/gin-gonic/gin"
)

// workbench_handle.go - 工作台页面与画布（编辑器壳 / 块与模板画布 / 模板预览）。

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
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrMissingPageID))
		return
	}
	// 走统一出口 pageOf：Detail 的 projectID 是必填的越权防护 scope，只传 ID 会被
	// 契约层判为「参数缺失」，而这里把它显示成「页面不存在」—— 一个真实的 404 与
	// 一个漏传 scope 的调用，在页面上长得一模一样。
	page, err := h.svc.PageByID(c.Request.Context(), pageID)
	if err != nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrPageNotFound))
		return
	}
	documentJSON, err := json.Marshal(page.DraftDocument)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrDraftEncodeFailed))
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
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrEditorMetaEncodeFailed))
		return
	}
	// 组件 Inspector 面板 schema（docs/02-C3）：声明式 Controls 驱动检查器表单，
	// 前端按 content/style/advanced 分组渲染，替代硬编码字段。
	// 插件组件 schema 合并（与内置同构，docs/06 §5：上传即出现在检查器）。
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaBuildFailed))
		return
	}
	if asm != nil {
		for t, data := range asm.InspectorSchemas {
			schemas[t] = data
		}
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaEncodeFailed))
		return
	}
	c.HTML(http.StatusOK, "workbench/layout", shell.Prepare(c, gin.H{
		"title":     workbenchservice.WorkbenchTitle(page, workbenchTrFunc(c)),
		"pageId":    page.ID,
		"isBlock":   false,
		"draftPath": page.DraftPath,
		"version":   page.DraftVersion,
		"document":  shell.JsonSafe(string(documentJSON)),
		"meta":      shell.JsonSafe(string(metaJSON)),
		"schemas":   shell.JsonSafe(string(schemasJSON)),
		"jsVer":     workbenchservice.StaticJSVersion(),
	}))
}

// workbenchBlock 全局块编辑模式：复用工作台画布与检查器，
// 保存走 /api/block/update（无发布链、无 URL），meta.saveBase 指示前端切换接口前缀。
func (h *Handle) workbenchBlock(c *gin.Context, blockID string) {
	block, err := h.svc.BlockByID(c.Request.Context(), blockID)
	if err != nil || block == nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrBlockNotFound))
		return
	}
	documentJSON, err := json.Marshal(block.Document)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrBlockEncodeFailed))
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
	// 白名单收敛在这一处（shell.LocalReturnPath）：不是站内相对路径的值直接**丢弃** ——
	// meta 里没有这个键，保存后留在工作台，与普通块编辑完全一样。这里不做兜底跳转，
	// 因为「没带 returnUrl」与「带了非法 returnUrl」对用户是同一件事：不该离开编辑器。
	// 消费侧（SaveBlockContent）会再校验一次：meta 只是搬运，不是信任边界。
	if back := shell.LocalReturnPath(c.Query("returnUrl")); back != "" {
		meta["returnUrl"] = back
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrEditorMetaEncodeFailed))
		return
	}
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaBuildFailed))
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaEncodeFailed))
		return
	}
	c.HTML(http.StatusOK, "workbench/layout", shell.Prepare(c, gin.H{
		"title":    workbenchShortText(c, workbenchenums.TitleBlockPrefix) + block.Name,
		"pageId":   block.ID,
		"isBlock":  true,
		"document": shell.JsonSafe(string(documentJSON)),
		"meta":     shell.JsonSafe(string(metaJSON)),
		"schemas":  shell.JsonSafe(string(schemasJSON)),
		"jsVer":    workbenchservice.StaticJSVersion(),
	}))
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
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrContentTemplateEditNotAssembled))
		return
	}
	// projectID 留空：这里只按 id 取模板（工程作用域由预览入口 previewTemplateTarget 负责）。
	tpl, err := h.svc.TemplateByID(c.Request.Context(), templateID, "")
	if err != nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrTemplateNotFound))
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
		// 占位符 {type} 由词条携带：译文顺序可能与中文不同，所以不在这里拼进句子。
		c.String(http.StatusBadRequest,
			strings.ReplaceAll(workbenchShortText(c, workbenchenums.ErrPreviewEntityIDRequired), "{type}", entityType))
		return
	}
	// 装配校验按模式分流：无实体模式走 page 编译管线，用不到模板预览端口。
	if !noEntity && h.templatePreview == nil {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrContentTemplateEditNotAssembled))
		return
	}
	if noEntity && h.pages == nil {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrStructureTemplatePreviewNotAssembled))
		return
	}
	projectID := strings.TrimSpace(c.Query("projectId"))
	documentJSON, err := json.Marshal(tpl.DraftDocument)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrTemplateEncodeFailed))
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
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrEditorMetaEncodeFailed))
		return
	}
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaBuildFailed))
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		c.String(http.StatusInternalServerError, workbenchShortText(c, workbenchenums.ErrComponentSchemaEncodeFailed))
		return
	}
	previewQS := workbenchservice.TemplatePreviewQuery(tpl.ID, entityType, entityID, projectID)
	c.HTML(http.StatusOK, "workbench/layout", shell.Prepare(c, gin.H{
		"title":      workbenchShortText(c, workbenchenums.TitleTemplatePrefix) + tpl.Name,
		"pageId":     tpl.ID,
		"isBlock":    false,
		"isTemplate": true,
		// 画布顶栏的模式提示：结构模板的预览不解析字段绑定，这件事要在编辑器里看得见
		//（否则作者会以为「页眉里该出现的商品名没出现」是渲染坏了）。
		"isStructureTemplate": noEntity,
		"document":            shell.JsonSafe(string(documentJSON)),
		"meta":                shell.JsonSafe(string(metaJSON)),
		"schemas":             shell.JsonSafe(string(schemasJSON)),
		"previewQS":           previewQS,
		"jsVer":               workbenchservice.StaticJSVersion(),
	}))
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
	templateID := strings.TrimSpace(c.PostForm("id"))
	document := json.RawMessage(c.PostForm("draftDocument"))
	if templateID == "" || len(document) == 0 {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrPreviewParamsIncomplete))
		return
	}
	projectID := c.PostForm("projectId")
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
	entityType := strings.TrimSpace(c.PostForm("entityType"))
	entityID := strings.TrimSpace(c.PostForm("entityId"))
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
		html = injectEditorBridge(html, shell.TranslateFor(c))
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

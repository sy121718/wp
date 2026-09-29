package workbenchhttp

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"

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
	page, err := h.pageOf(c, pageID)
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
	asm := h.pluginAssembly(c)
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
		"blocks": h.blockSummaries(c, page.ProjectID),
		// 全局设置面板：页面挂接的主题与当前设置（颜色/字体），可就地修改保存。
		"themeId":       h.themeIDOf(c, page),
		"themeSettings": h.themeSettingsOf(c, page),
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
		"title":     workbenchTitle(c, page),
		"pageId":    page.ID,
		"isBlock":   false,
		"draftPath": page.DraftPath,
		"version":   page.DraftVersion,
		"document":  shell.JsonSafe(string(documentJSON)),
		"meta":      shell.JsonSafe(string(metaJSON)),
		"schemas":   shell.JsonSafe(string(schemasJSON)),
		"jsVer":     workbenchJsVer(),
	}))
}

// workbenchJsVer 工作台脚本缓存版本：取拆分后模块目录（static/js/workbench/**）
// 下所有 .js 的最新 mtime。任一模块改动都会让入口 URL 的 ?v= 变化，配合
// StaticCacheMiddleware 的协商缓存，浏览器不会再执行旧模块。
func workbenchJsVer() string {
	root := filepath.Join("internal", "templates", "static", "js", "workbench")
	var latest int64
	err := filepath.Walk(root, func(_ string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // 目录缺失/权限问题不阻断渲染，版本退化为 0
		}
		if fi.IsDir() || !strings.HasSuffix(fi.Name(), ".js") {
			return nil
		}
		if m := fi.ModTime().Unix(); m > latest {
			latest = m
		}
		return nil
	})
	if err != nil || latest == 0 {
		return "0"
	}
	return strconv.FormatInt(latest, 10)
}

// workbenchBlock 全局块编辑模式：复用工作台画布与检查器，
// 保存走 /api/block/update（无发布链、无 URL），meta.saveBase 指示前端切换接口前缀。
func (h *Handle) workbenchBlock(c *gin.Context, blockID string) {
	block, err := h.blocks.Detail(c.Request.Context(), &blockcontract.DetailReq{ID: blockID})
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
		"jsVer":    workbenchJsVer(),
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
	if h.contenttemplates == nil {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrContentTemplateEditNotAssembled))
		return
	}
	tpl, err := h.contenttemplates.Get(c.Request.Context(), &contenttemplatedto.GetReq{ID: templateID})
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
	previewQS := templatePreviewQuery(tpl.ID, entityType, entityID, projectID)
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
		"jsVer":               workbenchJsVer(),
	}))
}

// templatePreviewQuery 模板画布 iframe 与「新标签预览」共用的查询串。
//
// entityId 为空（结构模板的无实体模式）时不带该参数：空串参数与服务端「缺参数」在
// 日志与排查里长得一样，少一个无意义的空参数省一次误判。
func templatePreviewQuery(templateID, entityType, entityID, projectID string) string {
	q := url.Values{}
	q.Set("template", templateID)
	q.Set("entityType", entityType)
	if entityID != "" {
		q.Set("entityId", entityID)
	}
	q.Set("editor", "1")
	if projectID != "" {
		q.Set("projectId", projectID)
	}
	return q.Encode()
}

// templateByID 按模板 id 取预览目标：优先用画布自己带过来的工程作用域
// （content_templates 带 FORCE 策略，作用域缺省时只能靠「工程唯一」解析）。
func (h *Handle) templateByID(c *gin.Context, templateID, projectID string) (*contenttemplatedto.TemplateResp, error) {
	if pid := strings.TrimSpace(projectID); pid != "" {
		return h.contenttemplates.GetScoped(c.Request.Context(), pid, templateID)
	}
	return h.contenttemplates.Get(c.Request.Context(), &contenttemplatedto.GetReq{ID: templateID})
}

// previewTemplateTarget 取预览目标模板；模板不存在时已写响应并返回 ok=false。
func (h *Handle) previewTemplateTarget(c *gin.Context, templateID, projectID string) (tpl *contenttemplatedto.TemplateResp, ok bool) {
	if h.contenttemplates == nil {
		c.String(http.StatusServiceUnavailable, workbenchShortText(c, workbenchenums.ErrContentTemplateEditNotAssembled))
		return nil, false
	}
	tpl, err := h.templateByID(c, templateID, projectID)
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
	h.renderPreview(c, document, projectID, "", withEditorBridge, previewDocStructureTemplate)
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
		// 其余归口：分类判据见 workbench_err.go 的 templatePreviewFacingKey。
		if key, ok := templatePreviewFacingKey(err.Error()); ok {
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

// blockSummaries 工程块列表的轻量投影（id/name/kind/category/reuseMode，不含文档大字段）。
// category 供 workbench 全局块按分类分组；reuseMode 供「引用/复制」双动作分流（docs/02-D §5.3）。
func (h *Handle) blockSummaries(c *gin.Context, projectID string) []gin.H {
	blocks, err := h.blocks.List(c.Request.Context(), &blockcontract.ListReq{ProjectID: projectID})
	if err != nil {
		return []gin.H{}
	}
	out := make([]gin.H, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, gin.H{"id": b.ID, "name": b.Name, "kind": b.Kind, "category": b.Category, "reuseMode": b.ReuseMode})
	}
	return out
}

// themeIDOf 页面挂接的主题 ID（未挂接返回空串）。
func (h *Handle) themeIDOf(c *gin.Context, page *pagecontract.PageResp) string {
	if page.ThemeID == "" {
		return ""
	}
	return page.ThemeID
}

// themeSettingsOf 页面挂接主题的 settings（colors/fontFamily 等），未挂接或查询失败返回 nil。
func (h *Handle) themeSettingsOf(c *gin.Context, page *pagecontract.PageResp) json.RawMessage {
	if page.ThemeID == "" {
		return nil
	}
	theme, err := h.projects.GetTheme(c.Request.Context(), page.ThemeID)
	if err != nil || theme == nil {
		return nil
	}
	return theme.Settings
}

package workbenchhttp

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
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
	pageID := strings.TrimSpace(c.Query("id"))
	if pageID == "" {
		c.String(http.StatusBadRequest, "缺少页面 id")
		return
	}
	// 走统一出口 pageOf：Detail 的 projectID 是必填的越权防护 scope，只传 ID 会被
	// 契约层判为「参数缺失」，而这里把它显示成「页面不存在」—— 一个真实的 404 与
	// 一个漏传 scope 的调用，在页面上长得一模一样。
	page, err := h.pageOf(c, pageID)
	if err != nil {
		c.String(http.StatusNotFound, "页面不存在")
		return
	}
	documentJSON, err := json.Marshal(page.DraftDocument)
	if err != nil {
		c.String(http.StatusInternalServerError, "草稿文档序列化失败")
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
		c.String(http.StatusInternalServerError, "编辑器元数据序列化失败")
		return
	}
	// 组件 Inspector 面板 schema（docs/02-C3）：声明式 Controls 驱动检查器表单，
	// 前端按 content/style/advanced 分组渲染，替代硬编码字段。
	// 插件组件 schema 合并（与内置同构，docs/06 §5：上传即出现在检查器）。
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		c.String(http.StatusInternalServerError, "组件 schema 生成失败")
		return
	}
	if asm != nil {
		for t, data := range asm.InspectorSchemas {
			schemas[t] = data
		}
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		c.String(http.StatusInternalServerError, "组件 schema 序列化失败")
		return
	}
	c.HTML(http.StatusOK, "workbench/layout", shell.Prepare(c, gin.H{
		"title":     workbenchTitle(page),
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
		c.String(http.StatusNotFound, "全局块不存在")
		return
	}
	documentJSON, err := json.Marshal(block.Document)
	if err != nil {
		c.String(http.StatusInternalServerError, "块文档序列化失败")
		return
	}
	metaJSON, err := json.Marshal(gin.H{
		// target 编辑目标描述符（EDT-017）。
		"target":    workbenchTargetOf(EditTargetBlock),
		"pageId":    block.ID, // 复用键名：前端保存逻辑按 saveBase 切换接口
		"projectId": block.ProjectID,
		"saveBase":  "block",
		"blockName": block.Name,
		"kind":      block.Kind,
		"draftPath": "",
		"version":   0,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, "编辑器元数据序列化失败")
		return
	}
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		c.String(http.StatusInternalServerError, "组件 schema 生成失败")
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		c.String(http.StatusInternalServerError, "组件 schema 序列化失败")
		return
	}
	c.HTML(http.StatusOK, "workbench/layout", shell.Prepare(c, gin.H{
		"title":    "编辑块：" + block.Name,
		"pageId":   block.ID,
		"isBlock":  true,
		"document": shell.JsonSafe(string(documentJSON)),
		"meta":     shell.JsonSafe(string(metaJSON)),
		"schemas":  shell.JsonSafe(string(schemasJSON)),
		"jsVer":    workbenchJsVer(),
	}))
}

// workbenchTemplate 内容模板编辑模式（EDT-001）：复用工作台画布与检查器，
// 保存走 /api/contenttemplate/update；预览经 presentation 用样例实体解析字段绑定。
func (h *Handle) workbenchTemplate(c *gin.Context, templateID string) {
	if h.contenttemplates == nil || h.templatePreview == nil {
		c.String(http.StatusServiceUnavailable, "内容模板编辑能力未装配")
		return
	}
	tpl, err := h.contenttemplates.Get(c.Request.Context(), &contenttemplatedto.GetReq{ID: templateID})
	if err != nil {
		c.String(http.StatusNotFound, "模板不存在")
		return
	}
	entityID := strings.TrimSpace(c.Query("entityId"))
	entityType := strings.TrimSpace(c.Query("entityType"))
	if entityType == "" {
		entityType = tpl.EntityType
	}
	projectID := strings.TrimSpace(c.Query("projectId"))
	if entityID == "" {
		c.String(http.StatusBadRequest, "缺少预览样例实体 entityId（字段绑定预览需要一条真实 "+entityType+" 记录）")
		return
	}
	documentJSON, err := json.Marshal(tpl.DraftDocument)
	if err != nil {
		c.String(http.StatusInternalServerError, "模板文档序列化失败")
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
		"projectId":    projectID,
		"draftPath":    "",
		"version":      tpl.DraftVersion,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, "编辑器元数据序列化失败")
		return
	}
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		c.String(http.StatusInternalServerError, "组件 schema 生成失败")
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		c.String(http.StatusInternalServerError, "组件 schema 序列化失败")
		return
	}
	previewQS := templatePreviewQuery(tpl.ID, entityType, entityID, projectID)
	c.HTML(http.StatusOK, "workbench/layout", shell.Prepare(c, gin.H{
		"title":      "编辑模板：" + tpl.Name,
		"pageId":     tpl.ID,
		"isBlock":    false,
		"isTemplate": true,
		"document":   shell.JsonSafe(string(documentJSON)),
		"meta":       shell.JsonSafe(string(metaJSON)),
		"schemas":    shell.JsonSafe(string(schemasJSON)),
		"previewQS":  previewQS,
		"jsVer":      workbenchJsVer(),
	}))
}

// templatePreviewQuery 模板画布 iframe 与「新标签预览」共用的查询串。
func templatePreviewQuery(templateID, entityType, entityID, projectID string) string {
	q := "template=" + templateID + "&entityType=" + entityType + "&entityId=" + entityID + "&editor=1"
	if projectID != "" {
		q += "&projectId=" + projectID
	}
	return q
}

// TemplatePreview 基于已保存模板 + 样例实体编译预览（画布 iframe GET）。
func (h *Handle) TemplatePreview(c *gin.Context) {
	if h.contenttemplates == nil || h.templatePreview == nil {
		c.String(http.StatusServiceUnavailable, "内容模板预览能力未装配")
		return
	}
	templateID := strings.TrimSpace(c.Query("template"))
	entityType := strings.TrimSpace(c.Query("entityType"))
	entityID := strings.TrimSpace(c.Query("entityId"))
	if templateID == "" || entityType == "" || entityID == "" {
		c.String(http.StatusBadRequest, "缺少 template / entityType / entityId")
		return
	}
	h.renderTemplatePreview(c, templateID, entityType, entityID, c.Query("projectId"), nil,
		c.Query("editor") == "1")
}

// TemplatePreviewDraft 基于未保存 AST + 样例实体返回临时预览（POST，画布刷新）。
func (h *Handle) TemplatePreviewDraft(c *gin.Context) {
	if h.contenttemplates == nil || h.templatePreview == nil {
		c.String(http.StatusServiceUnavailable, "内容模板预览能力未装配")
		return
	}
	templateID := strings.TrimSpace(c.PostForm("id"))
	entityType := strings.TrimSpace(c.PostForm("entityType"))
	entityID := strings.TrimSpace(c.PostForm("entityId"))
	document := json.RawMessage(c.PostForm("draftDocument"))
	if templateID == "" || entityType == "" || entityID == "" || len(document) == 0 {
		c.String(http.StatusBadRequest, "预览参数不完整")
		return
	}
	h.renderTemplatePreview(c, templateID, entityType, entityID, c.PostForm("projectId"), document, true)
}

func (h *Handle) renderTemplatePreview(c *gin.Context, templateID, entityType, entityID, projectID string,
	draftDocument json.RawMessage, withEditorBridge bool) {
	res, err := h.templatePreview.PreviewInstance(c.Request.Context(), &presentationdto.PreviewInstanceReq{
		EntityType: entityType, EntityID: entityID, TemplateID: templateID,
		ProjectID: projectID, DraftDocument: draftDocument,
	})
	if err != nil {
		// 422 保留（编译失败对作者是业务信息），但错误原文只进日志：
		// 编译器的错误里带节点路径与模板片段，直接铺在页面上等于把内部结构公开。
		if err != nil {
			logger.Scene("content_template").With("path", c.Request.URL.Path).Error(err, "模板编译失败")
		}
		c.String(http.StatusUnprocessableEntity, workbenchenums.MsgCompileFailed)
		return
	}
	html := res.HTML
	if withEditorBridge {
		html = injectEditorBridge(html)
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

// Package dashboardhttp 承载需要后端逻辑的后台页面入口（仪表盘与可视化编辑器）。
//
// 纯静态模板直接放 internal/templates；只有需要后端数据/逻辑的页面才落到本模块。
// 可视化编辑器（Visual Workbench，docs/03-A）外壳在本模块装配：
// 页面壳 + 草稿 AST 注入 + 预览编译直出。
package dashboardhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	plugincontract "go_wp/internal/module/plugin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/middleware/builtin"
	"go_wp/internal/templates"

	"github.com/CloudyKit/jet/v6"
	"go_wp/pkg/captcha"

	"github.com/gin-gonic/gin"
)

// Handle 页面处理器，聚合 dashboard 相关 handler。
type Handle struct {
	pages      pagecontract.PageService
	projects   projectcontract.ProjectService
	blocks     blockcontract.BlockService
	plugins    plugincontract.PluginService
	collection core.CollectionResolver
}

// NewHandle 创建页面处理器；pages/projects/blocks/plugins 为各模块契约，
// collection 为集合内容解析器（插件集合绑定预览渲染）。
func NewHandle(pages pagecontract.PageService, projects projectcontract.ProjectService,
	blocks blockcontract.BlockService, plugins plugincontract.PluginService,
	collection core.CollectionResolver) *Handle {
	return &Handle{pages: pages, projects: projects, blocks: blocks, plugins: plugins, collection: collection}
}

// Dashboard 仪表盘页面。
func (h *Handle) Dashboard(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/dashboard", withCSRF(c, gin.H{
		"title": dashboardenums.MsgDashboardTitle,
		"menu":  "dashboard",
	}))
}

// LoginPage 后台登录页（独立布局，供未登录的页面请求 302 跳转，也支持直接访问）。
// 验证码经 /api/captcha 返回图片（答案不下发），此处渲染页面骨架即可；
// 若渲染入口已生成验证码图片则直接注入，避免首屏额外请求。
func (h *Handle) LoginPage(c *gin.Context) {
	id, image := captcha.Get().GenerateImage()
	c.HTML(http.StatusOK, "admin/login", gin.H{
		"title":         "登录",
		"captcha_id":    id,
		"captcha_image": image,
	})
}

// withCSRF 向模板数据注入当前会话的 CSRF token（layout 的 hx-headers 使用）。
// token 获取失败时置空串：Jet 的 {{ .["csrf_token"] }} 对缺 key 安全输出空值，
// 不阻塞页面渲染；已登录用户正常流程下 token 必然存在（登录时已生成）。
func withCSRF(c *gin.Context, data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}
	if tok, err := builtin.GetCSRFToken(c); err == nil {
		data["csrf_token"] = tok
	} else {
		data["csrf_token"] = ""
	}
	return data
}

// Workbench 可视化编辑器外壳：注入 Page 草稿 AST 与保存接口所需元数据。
// ?block=ID 进入全局块编辑模式（同一画布，保存走块接口、无发布链）。
func (h *Handle) Workbench(c *gin.Context) {
	if blockID := strings.TrimSpace(c.Query("block")); blockID != "" {
		h.workbenchBlock(c, blockID)
		return
	}
	pageID := strings.TrimSpace(c.Query("id"))
	if pageID == "" {
		c.String(http.StatusBadRequest, "缺少页面 id")
		return
	}
	page, err := h.pages.Detail(c.Request.Context(), &pagedto.DetailReq{ID: pageID})
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
	var pluginComponents []plugindto.ComponentSummary
	var pluginPresets []plugindto.PresetSummary
	asm := h.pluginAssembly(c)
	if asm != nil {
		pluginComponents = asm.Components
		pluginPresets = asm.Presets
	}
	// 预设空时给空数组而非 nil（前端按数组读取，避免 undefined）。
	if pluginPresets == nil {
		pluginPresets = []plugindto.PresetSummary{}
	}
	metaJSON, err := json.Marshal(gin.H{
		"pageId":    page.ID,
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
	c.HTML(http.StatusOK, "workbench/layout", withCSRF(c, gin.H{
		"title":     workbenchTitle(page),
		"pageId":    page.ID,
		"isBlock":   false,
		"draftPath": page.DraftPath,
		"version":   page.DraftVersion,
		"document":  string(documentJSON),
		"meta":      string(metaJSON),
		"schemas":   string(schemasJSON),
		"jsVer":     workbenchJsVer(),
	}))
}

// jsVer 工作台脚本的缓存版本（文件 mtime），开发期改 JS 无需手动升版本号。
func workbenchJsVer() string {
	if fi, err := os.Stat(filepath.Join("internal", "templates", "static", "js", "workbench.js")); err == nil {
		return strconv.FormatInt(fi.ModTime().Unix(), 10)
	}
	return "0"
}

// workbenchBlock 全局块编辑模式：复用工作台画布与检查器，
// 保存走 /api/block/update（无发布链、无 URL），meta.saveBase 指示前端切换接口前缀。
func (h *Handle) workbenchBlock(c *gin.Context, blockID string) {
	block, err := h.blocks.Detail(c.Request.Context(), &blockdto.DetailReq{ID: blockID})
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
		"pageId":    block.ID, // 复用键名：前端保存逻辑按 saveBase 切换接口
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
	c.HTML(http.StatusOK, "workbench/layout", withCSRF(c, gin.H{
		"title":    "编辑块：" + block.Name,
		"pageId":   block.ID,
		"isBlock":  true,
		"document": string(documentJSON),
		"meta":     string(metaJSON),
		"schemas":  string(schemasJSON),
		"jsVer":    workbenchJsVer(),
	}))
}

// blockResolverAdapter 适配 block 契约为 builder.BlockResolver（预览内联 globalref）。
//
// ctx 为请求上下文（随请求取消传播，避免 Background 泄漏）；cache 为单次
// 编译内的块解析缓存——同一页面引用同一全局块多次时只查一次库，其余复用。
type blockResolverAdapter struct {
	h     *Handle
	ctx   context.Context
	cache map[string][]*core.Node
}

// ResolveBlockRoot 按块 ID 返回块文档 root 节点。
func (a blockResolverAdapter) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	if a.cache != nil {
		if nodes, ok := a.cache[blockID]; ok {
			return nodes, nil
		}
	}
	block, err := a.h.blocks.Detail(a.ctx, &blockdto.DetailReq{ID: blockID})
	if err != nil || block == nil {
		return nil, fmt.Errorf("全局块 %s 不可用", blockID)
	}
	page, err := builder.ParsePage(block.Document)
	if err != nil {
		return nil, err
	}
	if a.cache != nil {
		a.cache[blockID] = page.Root
	}
	return page.Root, nil
}

// blockSummaries 工程块列表的轻量投影（id/name/kind/category，不含文档大字段）。
// category 供 workbench 全局块按分类分组。
func (h *Handle) blockSummaries(c *gin.Context, projectID string) []gin.H {
	blocks, err := h.blocks.List(c.Request.Context(), &blockdto.ListReq{ProjectID: projectID})
	if err != nil {
		return []gin.H{}
	}
	out := make([]gin.H, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, gin.H{"id": b.ID, "name": b.Name, "kind": b.Kind, "category": b.Category})
	}
	return out
}

// themeIDOf 页面挂接的主题 ID（未挂接返回空串）。
func (h *Handle) themeIDOf(c *gin.Context, page *pagedto.PageResp) string {
	if page.ThemeID == "" {
		return ""
	}
	return page.ThemeID
}

// themeSettingsOf 页面挂接主题的 settings（colors/fontFamily 等），未挂接或查询失败返回 nil。
func (h *Handle) themeSettingsOf(c *gin.Context, page *pagedto.PageResp) json.RawMessage {
	if page.ThemeID == "" {
		return nil
	}
	theme, err := h.projects.GetTheme(c.Request.Context(), page.ThemeID)
	if err != nil || theme == nil {
		return nil
	}
	return theme.Settings
}

// Preview 基于已保存草稿轻量编译并在独立响应中输出完整 HTML 文档
// （0-A1 §4.2 隔离预览：不落盘、不影响线上产物）。
func (h *Handle) Preview(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("id"))
	if pageID == "" {
		c.String(http.StatusBadRequest, "缺少页面 id")
		return
	}
	page, err := h.pages.Detail(c.Request.Context(), &pagedto.DetailReq{ID: pageID})
	if err != nil {
		c.String(http.StatusNotFound, "页面不存在")
		return
	}
	h.renderPreview(c, page.DraftDocument, c.Query("editor") == "1")
}

// BlockPreview 全局块画布预览（工作台块编辑模式 iframe 内嵌）。
func (h *Handle) BlockPreview(c *gin.Context) {
	blockID := strings.TrimSpace(c.Query("id"))
	if blockID == "" {
		c.String(http.StatusBadRequest, "缺少块 id")
		return
	}
	block, err := h.blocks.Detail(c.Request.Context(), &blockdto.DetailReq{ID: blockID})
	if err != nil || block == nil {
		c.String(http.StatusNotFound, "全局块不存在")
		return
	}
	h.renderPreview(c, block.Document, c.Query("editor") == "1")
}

// PreviewDraft 基于未保存 AST 返回临时预览，不持久化、不写 Artifact、不影响发布指针。
func (h *Handle) PreviewDraft(c *gin.Context) {
	pageID := strings.TrimSpace(c.PostForm("id"))
	document := json.RawMessage(c.PostForm("draftDocument"))
	version, err := strconv.ParseInt(c.PostForm("expectedVersion"), 10, 64)
	if pageID == "" || err != nil || len(document) == 0 {
		c.String(http.StatusBadRequest, "草稿文档解析失败")
		return
	}
	page, err := h.pages.Detail(c.Request.Context(), &pagedto.DetailReq{ID: pageID})
	if err != nil {
		c.String(http.StatusNotFound, "页面不存在")
		return
	}
	if version != page.DraftVersion {
		c.String(http.StatusConflict, "草稿版本已更新，请刷新后重试")
		return
	}
	h.renderPreview(c, document, true)
}

// renderPreview 只完成 AST 校验与编译，响应生命周期结束即丢弃结果。
// 插件组件：启用插件集注入（CompositeSet 模板命名空间合并 + PluginResolver），
// 与正式构建同源（docs/06-plugin-system.md §10）。
func (h *Handle) renderPreview(c *gin.Context, document json.RawMessage, withEditorBridge bool) {
	var docPage *builder.Page
	if err := json.Unmarshal(document, &docPage); err != nil || docPage == nil {
		c.String(http.StatusBadRequest, "草稿文档解析失败")
		return
	}
	// 预览与正式构建同源：全局块引用按需展开——画布所见与产物一致。
	// 组件模板 Set：无启用插件走 embed 单例（hot path 缓存）；有插件按任务组装
	// CompositeSet（内置 embed + 插件命名空间合并，docs/06 §7）。
	set, serr := h.componentSet(c)
	if serr != nil {
		c.String(http.StatusInternalServerError, "组件模板 Set 加载失败: %s", serr.Error())
		return
	}
	opts := []builder.CompileOption{builder.WithContext(c.Request.Context()), builder.WithComponentSet(set)}
	if h.blocks != nil {
		// 请求 ctx + 单次编译内块缓存：重复 globalref 只查一次库。
		opts = append(opts, builder.WithBlockResolver(blockResolverAdapter{
			h:     h,
			ctx:   c.Request.Context(),
			cache: make(map[string][]*core.Node),
		}))
	}
	if asm := h.pluginAssembly(c); asm != nil {
		opts = append(opts, builder.WithPluginResolver(plugincontract.AssemblyResolver(asm)))
	}
	if h.collection != nil {
		opts = append(opts, builder.WithCollectionResolver(h.collection))
	}
	// 主题快照注入：settings.theme（Woodmart 级令牌）→ 编译进产物。
	if docPage.Settings.Theme != nil {
		opts = append(opts, builder.WithThemeSettings(docPage.Settings.Theme))
	}
	compiled, err := builder.Compile(docPage, opts...)
	if err != nil {
		c.String(http.StatusUnprocessableEntity, "编译失败: %s", err.Error())
		return
	}
	// 页眉/页脚块内联（settings.structure 绑定快照）：预览与正式构建同源，
	// 画布渲染页眉页脚，所见即所得（对齐 page service 的 assembleCompile）。
	headerHTML, headerCSS := h.compilePreviewBlock(c, docPage.Settings.Structure.HeaderBlockID)
	footerHTML, footerCSS := h.compilePreviewBlock(c, docPage.Settings.Structure.FooterBlockID)
	compiled.HTML = headerHTML + compiled.HTML + footerHTML
	compiled.CSS = headerCSS + compiled.CSS + footerCSS
	html, rerr := builder.RenderDocument(compiled)
	if rerr != nil {
		c.String(http.StatusInternalServerError, "文档渲染失败: %s", rerr.Error())
		return
	}
	if withEditorBridge {
		html = injectEditorBridge(html)
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

// compilePreviewBlock 编译页眉/页脚块片段（编辑器预览专用，与正式构建同源）。
// 块不存在/编译失败返回空片段（页眉页脚为可选，不阻塞预览）。
func (h *Handle) compilePreviewBlock(c *gin.Context, blockID string) (html, css string) {
	if blockID == "" || h.blocks == nil {
		return "", ""
	}
	block, err := h.blocks.Detail(c.Request.Context(), &blockdto.DetailReq{ID: blockID})
	if err != nil || block == nil || len(block.Document) == 0 {
		return "", ""
	}
	page, err := builder.ParsePage(block.Document)
	if err != nil {
		return "", ""
	}
	set, serr := templates.NewEmbeddedComponentSet()
	if serr != nil {
		return "", ""
	}
	compiled, cerr := builder.Compile(page, builder.WithContext(c.Request.Context()), builder.WithComponentSet(set))
	if cerr != nil {
		return "", ""
	}
	return compiled.HTML, compiled.CSS
}

// componentSet 组件模板 Set：无插件 → embed 单例；有插件 → CompositeSet。
func (h *Handle) componentSet(c *gin.Context) (*jet.Set, error) {
	asm := h.pluginAssembly(c)
	if asm == nil || len(asm.PluginFS) == 0 {
		return templates.NewEmbeddedComponentSet()
	}
	return templates.NewCompositeSet(asm.PluginFS)
}

// pluginAssembly 启用插件装配素材（无插件模块契约或无启用插件时返回 nil）。
// 单请求内缓存（避免 Workbench 渲染 + 编译重复查询）。
func (h *Handle) pluginAssembly(c *gin.Context) *plugincontract.Assembly {
	if h.plugins == nil {
		return nil
	}
	if v, ok := c.Get("pluginAssembly"); ok {
		if asm, ok := v.(*plugincontract.Assembly); ok {
			return asm
		}
	}
	asm, err := h.plugins.EnabledAssembly(c.Request.Context())
	if err != nil {
		return nil
	}
	if asm != nil && (len(asm.PluginFS) > 0 || len(asm.Specs) > 0) {
		c.Set("pluginAssembly", asm)
		return asm
	}
	return nil
}

func workbenchTitle(page *pagedto.PageResp) string {
	if page == nil || strings.TrimSpace(page.ID) == "" {
		return "可视化编辑器"
	}
	var doc struct {
		Settings struct {
			SEO struct {
				Title string `json:"title"`
			} `json:"seo"`
		} `json:"settings"`
	}
	_ = json.Unmarshal(page.DraftDocument, &doc)
	if doc.Settings.SEO.Title != "" {
		return doc.Settings.SEO.Title
	}
	return "编辑器 · " + page.DraftPath
}

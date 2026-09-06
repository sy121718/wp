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
	pluginservice "go_wp/internal/module/plugin/service"
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
	opts := []builder.CompileOption{builder.WithComponentSet(set)}
	if h.blocks != nil {
		// 请求 ctx + 单次编译内块缓存：重复 globalref 只查一次库。
		opts = append(opts, builder.WithBlockResolver(blockResolverAdapter{
			h:     h,
			ctx:   c.Request.Context(),
			cache: make(map[string][]*core.Node),
		}))
	}
	if asm := h.pluginAssembly(c); asm != nil {
		opts = append(opts, builder.WithPluginResolver(pluginservice.AssemblyResolver(asm)))
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
	html := builder.RenderDocument(compiled)
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
	compiled, cerr := builder.Compile(page, builder.WithComponentSet(set))
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

// editorBridgeScript 在 iframe 内运行的编辑器桥接脚本（仅编辑器预览注入）。
// 职责：节点选择标记还原、点击选中上报、选中高亮、容器/元素下方
// 「+ 插入组件」浮标、拖放落点指示线样式。
const editorBridgeScript = `<script>
(function(){
  // 编译器把节点 ID 编入 wp-c-* CSS 类；编辑器桥接层将其还原为选择标记。
  document.querySelectorAll('[class]').forEach(function(el){
    el.classList.forEach(function(cls){
      if (cls.indexOf('wp-c-') !== 0) return;
      el.setAttribute('data-wp-id', cls.slice(5));
    });
  });
  document.querySelectorAll('[id]').forEach(function(el){
    if (!el.getAttribute('data-wp-id')) el.setAttribute('data-wp-id', el.id);
  });
  document.querySelectorAll('[data-wp-id]').forEach(function(el){ el.setAttribute('draggable', 'true'); });

  // 画布内元素可直接拖动重排：与大纲树/组件库共用同一数据键。
  // 拖放落点在本桥接内计算（iframe 每次刷新必然重新注入，
  // 不依赖父窗口绑定时序），通过 wb-canvas-drop 消息交父窗口执行 AST 变更。
  var dropCtx = null;
  function clearDropMarks(){
    document.querySelectorAll('.wb-drop-before,.wb-drop-after,.wb-drop-inside').forEach(function(el){
      el.classList.remove('wb-drop-before','wb-drop-after','wb-drop-inside');
    });
  }
  document.addEventListener('dragstart', function(ev){
    var target = ev.target.closest ? ev.target.closest('[data-wp-id]') : null;
    if(!target) return;
    ev.dataTransfer.effectAllowed = 'move';
    ev.dataTransfer.setData('application/x-wb-node', target.getAttribute('data-wp-id'));
    target.style.opacity = '0.4';
    setTimeout(function(){ target.style.opacity = ''; }, 0);
  }, true);
  document.addEventListener('dragover', function(ev){
    var target = ev.target.closest ? ev.target.closest('[data-wp-id]') : null;
    clearDropMarks();
    if(!target) return;
    ev.preventDefault();
    var rect = target.getBoundingClientRect();
    var offset = ev.clientY - rect.top;
    var inMiddle = offset > rect.height * .3 && offset < rect.height * .7;
    var placement = inMiddle ? 'inside' : (offset < rect.height / 2 ? 'before' : 'after');
    // 容器判定由父窗口按 AST 进行；桥接按「有子元素且中带」粗略显示内部虚线。
    target.classList.add(placement === 'inside' ? 'wb-drop-inside' : (placement === 'before' ? 'wb-drop-before' : 'wb-drop-after'));
    dropCtx = { targetID: target.getAttribute('data-wp-id'), placement: placement, inMiddle: inMiddle, hasChildren: target.children.length > 0 };
  });
  document.addEventListener('dragleave', function(ev){
    if (!ev.relatedTarget) { clearDropMarks(); dropCtx = null; }
  });
  document.addEventListener('drop', function(ev){
    ev.preventDefault();
    clearDropMarks();
    var componentType = ev.dataTransfer.getData('application/x-wb-component');
    if (componentType) {
      // 组件库拖入：DataTransfer 归父窗口所有，交父窗口 bindCanvasDrop 处理。
      return;
    }
    var nodeID = ev.dataTransfer.getData('application/x-wb-node');
    if (!nodeID) return;
    var ctx = dropCtx || {};
    dropCtx = null;
    parent.postMessage({
      type: 'wb-canvas-drop',
      nodeID: nodeID,
      targetID: ctx.targetID || '',
      placement: ctx.placement || 'after',
      inMiddle: !!ctx.inMiddle,
      hasChildren: !!ctx.hasChildren
    }, location.origin);
  });

  var style = document.createElement('style');
  style.textContent = [
    '[data-wp-id]:hover{outline:1px solid rgba(37,99,235,.45);outline-offset:-1px;cursor:pointer;}',
    '[data-wp-id].wb-selected{outline:2px solid #2563eb;outline-offset:-2px;}',
    '.wb-bridge-insert{',
    '  position:absolute;z-index:99998;left:50%;transform:translateX(-50%);',
    '  padding:4px 12px;font-size:12px;line-height:1.6;white-space:nowrap;',
    '  color:#fff;background:#2563eb;border:none;border-radius:999px;cursor:pointer;',
    '  box-shadow:0 2px 10px rgba(37,99,235,.45);',
    '}',
    '.wb-bridge-insert:hover{background:#1d4ed8;}',
    '[data-wp-id].wb-drop-before{box-shadow:0 -3px 0 0 #2563eb;}',
    '[data-wp-id].wb-drop-after{box-shadow:0 3px 0 0 #2563eb;}',
    '[data-wp-id].wb-drop-inside{outline:2px dashed #2563eb;outline-offset:-2px;}'
  ].join('');
  document.head.appendChild(style);

  document.addEventListener('click', function(ev){
    var target = ev.target.closest('[data-wp-id]');
    if(!target) return;
    ev.preventDefault(); ev.stopPropagation();
    parent.postMessage({type:'wb-select', id: target.getAttribute('data-wp-id')}, location.origin);
  }, true);

  // 「+ 插入组件」浮标：父窗口在选中变化时发 wb-mark-selected，
  // 此处把浮标定位到选中元素底部中央；点击上报插入意图，
  // 由父窗口根据 AST 判断目标是容器(inside)还是普通元素(after)。
  var insertBtn = document.createElement('button');
  insertBtn.type = 'button';
  insertBtn.className = 'wb-bridge-insert';
  insertBtn.textContent = '+ 插入组件';
  insertBtn.style.display = 'none';
  document.body.appendChild(insertBtn);
  insertBtn.addEventListener('click', function(ev){
    ev.preventDefault(); ev.stopPropagation();
    var id = insertBtn.getAttribute('data-target-id') || '';
    if (id) parent.postMessage({type:'wb-insert-here', id: id}, location.origin);
  });

  window.addEventListener('message', function(ev){
    if (ev.origin !== location.origin || !ev.data) return;
    if (ev.data.type === 'wb-mark-selected') {
      var prev = document.querySelector('[data-wp-id].wb-selected');
      if (prev) prev.classList.remove('wb-selected');
      var el = ev.data.id ? document.querySelector('[data-wp-id="' + ev.data.id + '"]') : null;
      if (el) {
        el.classList.add('wb-selected');
        var rect = el.getBoundingClientRect();
        insertBtn.style.display = 'block';
        insertBtn.setAttribute('data-target-id', ev.data.id);
        insertBtn.style.top = (rect.bottom + window.scrollY + 4) + 'px';
      } else {
        insertBtn.style.display = 'none';
      }
    }
  });

  // ========== 画布直改三件套（对标 Figma/Elementor 就地编辑） ==========

  // 1) 双击就地编辑：文本类组件（heading/text/button/card 等）双击 →
  //    contenteditable 就地编辑 → 失焦/回车回写 AST（wb-edit-text 消息）。
  document.addEventListener('dblclick', function(ev){
    var target = ev.target.closest('[data-wp-id]');
    if(!target) return;
    ev.preventDefault(); ev.stopPropagation();
    // 已在编辑中不重复进入。
    if (target.isContentEditable) return;
    target.setAttribute('contenteditable', 'plaintext-only');
    target.focus();
    // 全选文本（就地替换习惯）。
    var range = document.createRange();
    range.selectNodeContents(target);
    var sel = window.getSelection();
    sel.removeAllRanges(); sel.addRange(range);
    target.classList.add('wb-editing');
    function finish(save){
      target.removeAttribute('contenteditable');
      target.classList.remove('wb-editing');
      target.removeEventListener('blur', onBlur);
      target.removeEventListener('keydown', onKey);
      if (save) {
        parent.postMessage({
          type: 'wb-edit-text',
          id: target.getAttribute('data-wp-id'),
          text: target.textContent.trim()
        }, location.origin);
      } else {
        // 取消：下次画布刷新自动还原（不主动刷新，等下次交互）。
      }
    }
    function onBlur(){ finish(true); }
    function onKey(e){
      if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); target.blur(); }
      if (e.key === 'Escape') { e.preventDefault(); finish(false); }
    }
    target.addEventListener('blur', onBlur);
    target.addEventListener('keydown', onKey);
  });

  // 2) 画布右键菜单：编辑/复制/粘贴到内部/删除/上移/下移/隐藏 + 动效快捷项。
  var ctxMenu = null;
  function closeCtxMenu(){ if (ctxMenu) { ctxMenu.remove(); ctxMenu = null; } }
  document.addEventListener('contextmenu', function(ev){
    var target = ev.target.closest('[data-wp-id]');
    closeCtxMenu();
    if(!target) return; // 画布空白处不拦截（浏览器原生菜单）。
    ev.preventDefault(); ev.stopPropagation();
    var id = target.getAttribute('data-wp-id');
    parent.postMessage({type:'wb-select', id: id}, location.origin);
    ctxMenu = document.createElement('div');
    ctxMenu.className = 'wb-ctx-menu';
    function item(label, action){
      var b = document.createElement('button');
      b.type = 'button'; b.textContent = label;
      b.addEventListener('click', function(e){ e.stopPropagation(); closeCtxMenu(); action(); });
      ctxMenu.appendChild(b);
    }
    function separator(){ var s = document.createElement('div'); s.className='wb-ctx-sep'; ctxMenu.appendChild(s); }
    function send(msg){ parent.postMessage(msg, location.origin); }
    item('✏️ 编辑文本', function(){ // 触发双击编辑。
      var el = document.querySelector('[data-wp-id="' + id + '"]');
      if (el) { var d = new MouseEvent('dblclick', {bubbles:true}); el.dispatchEvent(d); }
    });
    item('⧉ 复制', function(){ send({type:'wb-ctx', id:id, op:'copy'}); });
    item('✂ 剪切', function(){ send({type:'wb-ctx', id:id, op:'cut'}); });
    item('📋 粘贴到内部', function(){ send({type:'wb-ctx', id:id, op:'paste-inside'}); });
    separator();
    item('↑ 上移', function(){ send({type:'wb-ctx', id:id, op:'move-up'}); });
    item('↓ 下移', function(){ send({type:'wb-ctx', id:id, op:'move-down'}); });
    separator();
    // 动效快捷子项（效果基本库入口：常用 4 种入场 + 悬浮）。
    var anim = document.createElement('div'); anim.className='wb-ctx-group'; anim.textContent='✨ 入场动画';
    ctxMenu.appendChild(anim);
    ['fade-up','zoom-in','slide-up','blur-in'].forEach(function(eff){
      item('　' + eff, function(){ send({type:'wb-ctx', id:id, op:'entrance', value:eff}); });
    });
    item('🌀 悬浮上浮', function(){ send({type:'wb-ctx', id:id, op:'hover', value:'lift'}); });
    separator();
    item('🗑 删除', function(){ send({type:'wb-ctx', id:id, op:'delete'}); });
    document.body.appendChild(ctxMenu);
    // 定位（不越界）。
    var x = Math.min(ev.pageX, window.innerWidth - 180);
    var y = Math.min(ev.pageY, window.innerHeight - 320);
    ctxMenu.style.left = x + 'px'; ctxMenu.style.top = y + 'px';
  });
  document.addEventListener('click', function(ev){
    if (ctxMenu && !ctxMenu.contains(ev.target)) closeCtxMenu();
  }, true);

  // 3) 选中悬浮快捷条（Elementor 式小工具条：编辑/复制/删除）。
  var quickBar = document.createElement('div');
  quickBar.className = 'wb-quick-bar';
  quickBar.style.display = 'none';
  document.body.appendChild(quickBar);
  function positionQuickBar(el){
    var rect = el.getBoundingClientRect();
    quickBar.style.display = 'flex';
    quickBar.style.left = rect.left + 'px';
    quickBar.style.top = (rect.top - 30 + window.scrollY) + 'px';
    quickBar.setAttribute('data-target-id', el.getAttribute('data-wp-id'));
  }
  window.addEventListener('message', function(ev){
    if (ev.origin !== location.origin || !ev.data) return;
    if (ev.data.type === 'wb-mark-selected') {
      var el = ev.data.id ? document.querySelector('[data-wp-id="' + ev.data.id + '"]') : null;
      if (el) positionQuickBar(el); else quickBar.style.display = 'none';
    }
  });
  [['✏️','编辑',function(){ var el=document.querySelector('[data-wp-id="'+quickBar.getAttribute('data-target-id')+'"]'); if(el) el.dispatchEvent(new MouseEvent('dblclick',{bubbles:true})); }],
   ['⧉','复制',function(){ send2({type:'wb-ctx', id:quickBar.getAttribute('data-target-id'), op:'copy'}); }],
   ['🗑','删除',function(){ send2({type:'wb-ctx', id:quickBar.getAttribute('data-target-id'), op:'delete'}); }]
  ].forEach(function(t){
    var b = document.createElement('button');
    b.type='button'; b.textContent=t[0]; b.title=t[1];
    b.addEventListener('click', function(e){ e.stopPropagation(); t[2](); });
    quickBar.appendChild(b);
  });
  function send2(msg){ parent.postMessage(msg, location.origin); }

  // 直改样式（右键菜单/快捷条/编辑态）。
  var directStyle = document.createElement('style');
  directStyle.textContent = [
    '[data-wp-id].wb-editing{outline:2px solid #3d444f !important;cursor:text;}',
    '[contenteditable]{outline-offset:-2px;}',
    '.wb-ctx-menu{position:absolute;z-index:99999;min-width:160px;background:#fff;',
    '  border:1px solid #e5e7eb;border-radius:8px;box-shadow:0 8px 24px rgba(0,0,0,.14);',
    '  padding:4px;font-size:13px;color:#1a1d21;}',
    '.wb-ctx-menu button{display:block;width:100%;text-align:left;padding:6px 10px;',
    '  border:none;background:none;cursor:pointer;border-radius:6px;font-size:13px;color:inherit;}',
    '.wb-ctx-menu button:hover{background:#eceef1;}',
    '.wb-ctx-sep{height:1px;background:#e5e7eb;margin:4px 0;}',
    '.wb-ctx-group{padding:6px 10px 2px;font-size:11px;color:#6b7280;font-weight:600;}',
    '.wb-quick-bar{position:absolute;z-index:99998;display:none;gap:2px;',
    '  background:#1a1d21;border-radius:6px;padding:3px;box-shadow:0 4px 12px rgba(0,0,0,.25);}',
    '.wb-quick-bar button{border:none;background:none;cursor:pointer;font-size:13px;',
    '  padding:4px 8px;border-radius:4px;color:#fff;}',
    '.wb-quick-bar button:hover{background:rgba(255,255,255,.15);}'
  ].join('');
  document.head.appendChild(directStyle);

  // 拖放落点指示：父窗口 bindCanvasDrop 在 dragover 时给目标加类，
  // 这里只负责样式；drop/dragleave 时父窗口负责移除。
})();
</script>`

// injectEditorBridge 把编辑器桥接脚本追加到 </body> 前。
func injectEditorBridge(html string) string {
	idx := strings.LastIndex(html, "</body>")
	if idx < 0 {
		return html
	}
	return html[:idx] + editorBridgeScript + html[idx:]
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

// MediaPage 媒体库页面（左树右库：无限级分类筛选 + WP 式媒体网格/列表）。
// 页面骨架由模板渲染，数据与交互由 media-admin.js 驱动（复用 /api/media/*）。
func (h *Handle) MediaPage(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/media", withCSRF(c, gin.H{
		"title": "媒体库",
		"menu":  "media",
		"jsVer": mediaLibJsVer(),
	}))
}

// mediaLibJsVer 媒体库脚本缓存版本（media-lib.js / media-admin.js 中较新的 mtime）。
func mediaLibJsVer() string {
	latest := int64(0)
	for _, name := range []string{"media-lib.js", "media-admin.js"} {
		if fi, err := os.Stat(filepath.Join("internal", "templates", "static", "js", name)); err == nil && fi.ModTime().Unix() > latest {
			latest = fi.ModTime().Unix()
		}
	}
	if latest > 0 {
		return strconv.FormatInt(latest, 10)
	}
	return "0"
}

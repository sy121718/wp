// Package dashboardhttp 承载需要后端逻辑的后台页面入口（仪表盘与可视化编辑器）。
//
// 纯静态模板直接放 internal/templates；只有需要后端数据/逻辑的页面才落到本模块。
// 可视化编辑器（Visual Workbench，docs/03-A）外壳在本模块装配：
// 页面壳 + 草稿 AST 注入 + 预览编译直出。
package dashboardhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	admincontract "go_wp/internal/module/admin/contract"
	blockcontract "go_wp/internal/module/block/contract"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/pkg/logger"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/middleware/builtin"

	"go_wp/pkg/captcha"

	"github.com/gin-gonic/gin"
)

// Handle 页面处理器，聚合 dashboard 相关 handler。
// admin 六领域 CRUD 契约供 /admin 六领域管理页消费（管理员/角色/菜单/权限/部门/数据权限）。
type Handle struct {
	pages      pagecontract.PageService
	projects   projectcontract.ProjectService
	blocks     blockcontract.BlockService
	plugins    plugincontract.PluginService
	collection core.CollectionResolver
	admins     admincontract.AdminService
	roles      admincontract.RoleService
	perms      admincontract.PermService
	menus      admincontract.MenuService
	depts      admincontract.DeptService
	rules      admincontract.RuleService
	authz      admincontract.AuthzContextService
	// navigations 公开站点导航契约（导航菜单管理页，与后台权限菜单严格隔离）。
	navigations navigationcontract.NavigationService

	// contentStore 内容译文读写端口（翻译工作台，多语言 P5c）。
	// 为 nil 时按默认实现（pkg/i18n.ContentWriter + 默认数据库）惰性构造；
	// 测试经 SetContentTranslationStore 注入隔离 schema 的写入器。
	contentStore contentTranslationPort
	// siteIndex 全站可翻译内容索引缓存（跨页面复用提示 + 全站完成度，见
	// page_translations_index.go）。
	siteIndex siteContentIndexCache
}

// NewHandle 创建页面处理器；pages/projects/blocks/plugins 为各模块契约。
// collection 为集合内容解析器：预览编译下沉 page 模块后（renderPreview → CompilePreview），
// dashboard 不再直接使用，字段保留以维持 SetupDashboardRoutes 装配签名稳定（routes.go）。
func NewHandle(pages pagecontract.PageService, projects projectcontract.ProjectService,
	blocks blockcontract.BlockService, plugins plugincontract.PluginService,
	collection core.CollectionResolver,
	admins admincontract.AdminService, roles admincontract.RoleService,
	perms admincontract.PermService, menus admincontract.MenuService,
	depts admincontract.DeptService, rules admincontract.RuleService,
	authz admincontract.AuthzContextService,
	navigations navigationcontract.NavigationService) *Handle {
	h := &Handle{pages: pages, projects: projects, blocks: blocks, plugins: plugins, collection: collection,
		admins: admins, roles: roles, perms: perms, menus: menus, depts: depts, rules: rules, authz: authz,
		navigations: navigations}
	// 注入 block 服务 stale 传播器：块内容变更/删除后编排引用页面待重建。
	// 经匿名接口断言 SetStalePropagator 而非 import block service 实现（模块隔离）；
	// dashboard 在 page 之后装配，具备传播所需的 page/project 契约，打破 block↔page 装配循环。
	if setter, ok := blocks.(interface {
		SetStalePropagator(func(context.Context, string) error)
	}); ok {
		setter.SetStalePropagator(h.markStaleForBlockCtx)
	}
	// 注入引用检查器：global 块删除 / global→template 切换前判断是否仍被引用
	//（docs/02-D §9）。引用路径与 stale 传播一致：globalref/structure 页面 + 主题槽位。
	if setter, ok := blocks.(interface {
		SetReferenceChecker(func(context.Context, string) (bool, error))
	}); ok {
		setter.SetReferenceChecker(h.blockReferencedCtx)
	}
	return h
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
	// 登录页无会话（不走 withCSRF），同样需要语言数据：注入 lang/t/langs 供页面文案与语言切换使用。
	// 标题走 shell.login.title（缺词条回退「登录」）。
	c.HTML(http.StatusOK, "admin/login", withI18n(c, gin.H{
		"title":         translateFor(c)("shell.login.title", "登录"),
		"captcha_id":    id,
		"captcha_image": image,
	}))
}

// withCSRF 向模板数据注入当前会话的 CSRF token（layout 的 hx-headers 使用）。
// token 获取失败时置空串：Jet 的 {{ .["csrf_token"] }} 对缺 key 安全输出空值，
// 不阻塞页面渲染；已登录用户正常流程下 token 必然存在（登录时已生成）。
func withCSRF(c *gin.Context, data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}
	// 多语言：注入 lang / t / langs / lang_redirect，并把 title（enums key）翻成当前语言。
	// 缺词条时 t 回退模板内中文原文，绝不报错（见 i18n.go）。
	data = withI18n(c, data)
	if tok, err := builtin.GetCSRFToken(c); err == nil {
		data["csrf_token"] = tok
	} else {
		data["csrf_token"] = ""
	}
	// 当前用户有效权限码集合（permContextMiddleware 注入）。
	// 模板用 {{if .PermSet["role:list"]}} 控制菜单/按钮/字段显示，
	// 与 Casbin API 鉴权同源，避免「看得到但点不了」。
	set := map[string]bool{}
	if v, ok := c.Get(permSetKey); ok {
		if m, ok := v.(map[string]bool); ok {
			set = m
		}
	}
	data["PermSet"] = set
	// 侧边栏导航树（按权限过滤 + 当前页标记）。
	// 子页面（如翻译工作台）经 navPathFor 归到所属菜单项，避免整组失去高亮。
	navGroups := buildNav(set, navPathFor(c.Request.URL.Path))
	data["NavGroups"] = navGroups
	// 侧边栏展开态：由 cookie 决定，服务端渲染首屏即正确（无「先展开后收起」闪烁）。
	// 点击菜单导航时前端写 cookie=0，固定（pin）时写 cookie 且不再自动收起。
	open := sidebarOpen(c)
	pinned := sidebarPinned(c)
	// 当前页不属于任何目录分组（如「仪表盘」这类直接链接页）且未固定时默认收起，
	// 否则二级栏无 is-active 分组会整块空白。
	if open && !pinned && !hasActiveDirGroup(navGroups) {
		open = false
	}
	data["SidebarOpen"] = open
	data["SidebarPinned"] = pinned
	// 二级栏渲染开关：仅当当前页属于某个目录分组时才渲染二级栏 DOM。
	// 仪表盘这类直接链接页不渲染（无空栏占位）；此时点一级目录图标由 admin.js
	// 跳转到该组第一个页面（data-first-url），目标页正常渲染二级栏。
	data["HasSubnav"] = hasActiveDirGroup(navGroups)
	return data
}

// sidebarCookieOpen / sidebarCookiePinned 侧边栏状态 cookie 名。
const (
	sidebarCookieOpen   = "sidebar_open"
	sidebarCookiePinned = "sidebar_pinned"
)

// hasActiveDirGroup 判断导航树中是否存在「当前页所在的目录分组」（有二级内容的组）。
// 仪表盘这类直接链接页返回 false。
func hasActiveDirGroup(groups []navGroup) bool {
	for _, g := range groups {
		if g.Active && len(g.Nodes) > 0 {
			return true
		}
	}
	return false
}

// sidebarOpen 侧边栏是否展开：cookie 缺省为展开（首次访问体验）。
func sidebarOpen(c *gin.Context) bool {
	v, err := c.Cookie(sidebarCookieOpen)
	if err != nil || v == "" {
		return true
	}
	return v != "0"
}

// sidebarPinned 侧边栏是否固定（固定后导航不再自动收起）。
func sidebarPinned(c *gin.Context) bool {
	v, _ := c.Cookie(sidebarCookiePinned)
	return v == "1"
}

// permSetKey 请求上下文中的权限码集合键。
const permSetKey = "perm_set"

// permContextMiddleware 把当前用户有效权限码集合写入请求上下文（仅 dashboard 页面路由挂载）。
//
// API 鉴权由 Casbin 中间件独立负责，两者共用同一权限来源；
// 权限码查询失败时降级为空集合（页面照常渲染，仅不显示需权限的元素）。
func permContextMiddleware(authz admincontract.AuthzContextService) gin.HandlerFunc {
	return func(c *gin.Context) {
		set := map[string]bool{}
		if authz != nil {
			if v, ok := c.Get("user_id"); ok {
				if uid, ok := v.(int64); ok && uid > 0 {
					codes, aerr := authz.EffectivePermissionCodes(c.Request.Context(), uint64(uid))
					if aerr != nil {
						// 失败即降级为空权限集：本页所有需要权限的按钮与菜单都会消失，
						// 功能上等同于只读。用户看到的是「按钮不见了」，必须留痕才能定位。
						logger.Scene("dashboard").With("userId", uid).
							Error(aerr, "权限上下文查询失败，本页按空权限集渲染")
					}
					for _, code := range codes {
						set[code] = true
					}
				}
			}
		}
		c.Set(permSetKey, set)
		c.Next()
	}
}

// jsonSafe 转义 JSON 字符串中的 script 闭合序列，防止用户可编辑的 Page Document
// 注入 </script> 提前闭合 <script type="application/json"> 数据岛造成存储型 XSS。
// JSON 中 \/ 是合法转义（JSON.parse 会还原为 /，不影响前端解析），
// 仅处理 </ 与 <!-- 两个闭合点：其余 < 在 JSON 字符串内合法且不会闭合 script。
func jsonSafe(s string) string {
	s = strings.ReplaceAll(s, "</", `<\/`)
	s = strings.ReplaceAll(s, "<!--", `<\!--`)
	return s
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
	page, err := h.pages.Detail(c.Request.Context(), &pagecontract.DetailReq{ID: pageID})
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
		"document":  jsonSafe(string(documentJSON)),
		"meta":      jsonSafe(string(metaJSON)),
		"schemas":   jsonSafe(string(schemasJSON)),
		"jsVer":     workbenchJsVer(),
	}))
}

// jsVer 工作台脚本的缓存版本（文件 mtime），开发期改 JS 无需手动升版本号。
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
		"document": jsonSafe(string(documentJSON)),
		"meta":     jsonSafe(string(metaJSON)),
		"schemas":  jsonSafe(string(schemasJSON)),
		"jsVer":    workbenchJsVer(),
	}))
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

// Preview 基于已保存草稿轻量编译并在独立响应中输出完整 HTML 文档
// （0-A1 §4.2 隔离预览：不落盘、不影响线上产物）。
func (h *Handle) Preview(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("id"))
	if pageID == "" {
		c.String(http.StatusBadRequest, "缺少页面 id")
		return
	}
	page, err := h.pages.Detail(c.Request.Context(), &pagecontract.DetailReq{ID: pageID})
	if err != nil {
		c.String(http.StatusNotFound, "页面不存在")
		return
	}
	h.renderPreview(c, page.DraftDocument, page.ProjectID, page.DraftPath, c.Query("editor") == "1")
}

// BlockPreview 全局块画布预览（工作台块编辑模式 iframe 内嵌）。
func (h *Handle) BlockPreview(c *gin.Context) {
	blockID := strings.TrimSpace(c.Query("id"))
	if blockID == "" {
		c.String(http.StatusBadRequest, "缺少块 id")
		return
	}
	block, err := h.blocks.Detail(c.Request.Context(), &blockcontract.DetailReq{ID: blockID})
	if err != nil || block == nil {
		c.String(http.StatusNotFound, "全局块不存在")
		return
	}
	h.renderPreview(c, block.Document, block.ProjectID, "", c.Query("editor") == "1")
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
	page, err := h.pages.Detail(c.Request.Context(), &pagecontract.DetailReq{ID: pageID})
	if err != nil {
		c.String(http.StatusNotFound, "页面不存在")
		return
	}
	if version != page.DraftVersion {
		c.String(http.StatusConflict, "草稿版本已更新，请刷新后重试")
		return
	}
	h.renderPreview(c, document, page.ProjectID, page.DraftPath, true)
}

// renderPreview 只完成 AST 校验与编译，响应生命周期结束即丢弃结果。
// 编译复用 page 模块 CompilePreview（与正式构建同源装配管线，docs/06 §10）：
// 全局块引用展开、插件组件集注入、主题/集合解析均与构建一致，画布所见即产物。
// editorBridge（画布联动 JS）为后处理拼接，与编译无关，仅预览启用。
// projectID 为文档所属站点工程（页面/块的记录字段），驱动导航等站点级资源解析；
// currentPath 为页面访问路径（导航当前项高亮，块预览传空）。
func (h *Handle) renderPreview(c *gin.Context, document json.RawMessage, projectID, currentPath string, withEditorBridge bool) {
	// 预览语言：?lang= 显式指定（工作台多语言预览切换），空 = 站点默认语言。
	html, err := h.pages.CompilePreview(c.Request.Context(), document, projectID, currentPath, c.Query("lang"))
	if err != nil {
		switch {
		case errors.Is(err, pagecontract.ErrPreviewInvalidDocument):
			c.String(http.StatusBadRequest, "草稿文档解析失败")
		case errors.Is(err, pagecontract.ErrPreviewCompileFailed):
			// 编译校验错误是「用户可操作的配置提示」（如「轮播至少需要一个 slide」），
			// 透出具体原因并带节点定位，便于在工作台直接定位坏组件；
			// 仅剥掉内部包装前缀，系统级错误仍走统一提示（不泄露内部细节）。
			reason := err.Error()
			// 依次剥掉包装前缀（ErrPreviewCompileFailed 与 pipeline 的「页面编译失败」），
			// 只留用户可读的节点定位信息。
			reason = strings.TrimPrefix(reason, fmt.Sprint(pagecontract.ErrPreviewCompileFailed)+": ")
			reason = strings.TrimPrefix(reason, "页面编译失败: ")
			c.String(http.StatusUnprocessableEntity, dashboardenums.MsgCompileFailed+"："+reason)
		default:
			c.String(http.StatusInternalServerError, dashboardenums.MsgInternalError)
		}
		return
	}
	if withEditorBridge {
		html = []byte(injectEditorBridge(string(html)))
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
}

// pluginAssembly 启用插件装配素材（无插件模块契约或无启用插件时返回 nil）。
// 单请求内缓存（同一请求多次调用只查一次库）。
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

func workbenchTitle(page *pagecontract.PageResp) string {
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

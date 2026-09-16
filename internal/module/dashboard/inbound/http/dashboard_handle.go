package dashboardhttp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"go_wp/config"

	admincontract "go_wp/internal/module/admin/contract"
	blockcontract "go_wp/internal/module/block/contract"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/builder/core"

	"go_wp/pkg/captcha"

	"github.com/gin-gonic/gin"
)

// Package dashboardhttp 承载需要后端逻辑的后台页面入口（仪表盘与可视化编辑器）。

//

// 纯静态模板直接放 internal/templates；只有需要后端数据/逻辑的页面才落到本模块。

// 可视化编辑器（Visual Workbench，docs/03-A）外壳在本模块装配：

// 页面壳 + 草稿 AST 注入 + 预览编译直出。

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
	// products 商品构建期数据源（检查器 entityref 下拉取 CollectionFilterOptions）。
	products productcontract.ProductDataSource

	// contentStore 内容译文读写端口（翻译工作台，多语言 P5c）。
	// 为 nil 时按默认实现（pkg/i18n.ContentWriter + 默认数据库）惰性构造；
	// 测试经 SetContentTranslationStore 注入隔离 schema 的写入器。
	contentStore contentTranslationPort
	// siteIndex 全站可翻译内容索引缓存（跨页面复用提示 + 全站完成度，见
	// page_translations_index.go）。
	siteIndex siteContentIndexCache

	// templates / templatePreview 内容模板可视化编辑（EDT-001）：workbench?template=
	// 加载模板草稿，保存走 contenttemplate.Update，预览走 presentation.PreviewInstance。
	templates       contenttemplatecontract.ContentTemplateService
	templatePreview TemplatePreviewPort

	// blueprints 蓝图候选（新建页面时的空白草稿模板）。
	// 可空：端口未注入时表单不显示蓝图选项，建页照常（blueprintOptions 返回空切片）。
	blueprints blueprintcontract.BlueprintService
}

// SetBlueprints 注入蓝图契约（装配期调用；可空）。
func (h *Handle) SetBlueprints(b blueprintcontract.BlueprintService) { h.blueprints = b }

// pageOf 按 id 读取页面（草稿保存与页面翻译页共用）。
//
// 抽成一个方法是因为三处调用需要同一份「页面不存在怎么回」的语义：
// 它们各自决定跳转目标（列表页 / 404 / 回本页），但取数口径必须一致 ——
// 曾经这里有一处直接调 Detail 而忘了判空，页面被删后成了 500。
func (h *Handle) pageOf(c *gin.Context, pageID string) (*pagecontract.PageResp, error) {
	if h.pages == nil {
		return nil, errors.New("页面服务未装配")
	}
	// Detail 把 projectID 当**必填的越权防护 scope**（少它只会得到「参数缺失」，
	// 看起来像「页面不存在」）。画布 / 历史 / 译文这些路由手上只有 pageId，
	// 所以先用只读的 ProjectOfPage 问「这个页面属于谁」，再按 scope 取详情。
	ctx := c.Request.Context()
	projectID, err := h.pages.ProjectOfPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	return h.pages.Detail(ctx, &pagecontract.DetailReq{ProjectID: projectID, ID: pageID})
}

// TemplatePreviewPort 模板工作台预览所需的最窄 presentation 能力。
type TemplatePreviewPort interface {
	PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error)
}

// SetTemplateWorkbenchDeps 注入内容模板编辑与预览契约（装配期调用）。
func (h *Handle) SetTemplateWorkbenchDeps(templates contenttemplatecontract.ContentTemplateService,
	preview TemplatePreviewPort) {
	h.templates = templates
	h.templatePreview = preview
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
	if wired, ok := blocks.(interface{ RequireWiring() }); ok {
		wired.RequireWiring()
	}
	return h
}

// SetProductDataSource 注入商品构建期数据源（检查器 entityref 下拉，EDT-005）。
func (h *Handle) SetProductDataSource(p productcontract.ProductDataSource) {
	h.products = p
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
		// debug 模式才显示一键登录入口（release 下路由压根不存在，显示了也是个死链）。
		"DevLogin": devLoginEnabled(),
	}))
}

// devLoginEnabled 是否处于 debug 模式（决定登录页是否显示一键登录入口）。
//
// 与 routers 里注册 /admin/dev-login 用的是同一个判断：两处必须一致，
// 否则会出现「显示了链接但路由不存在」（release 下点进去 404）。
func devLoginEnabled() bool {
	v, err := config.GetViper()
	return err == nil && strings.EqualFold(v.GetString("server.mode"), "debug")
}

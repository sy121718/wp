package pagehttp

// page_router.go — page 模块**唯一的**路由声明点，同时也是模块装配的入口。
//
// 两条硬规则（改造时都踩反过一次，写在这里防回退）：
//
//  1. **路由只有一份**。本模块全部入口 —— `/api/page/*` 的接口面与 `/admin/*`、
//     `/workbench/*` 的页面面 —— 都只在这个文件里声明。散到 `page_<功能>_router.go`
//     之后，回答「这个模块一共暴露了哪些入口」要跨九个文件拼，而路由正是权限点 /
//     CSRF / Casbin 的对账面：漏看一条的表现不是编译错误，是「接口能删、页面上删不掉」
//     这类两边都不报错、测试也不红的分叉。
//     **按功能拆的是控制器**（`pages_handle.go` = 页面本体两面 + 版本历史、`page_translations_handle.go` =
//     译文三件套、`page_langs_handle.go` / `page_schedule_handle.go` / `page_redirect.go` / `page_site_slot_handle.go` /
//     `page_stale.go` 各一个功能、`page_jump.go` = 共享出口原语；
//     一个功能一个文件，里面既有页面 handler 也有 JSON 接口 handler），不是路由。
//
//  2. **同一功能的页面与接口挨着写**。一个业务动作有两条通道：JSON 接口（工作台 / 脚本 /
//     批量）与后台页面（运营）。它们在权限点路径、拒绝规则、访问面下线动作上**必须同源**，
//     而 `<模块>_router.go`（接口）+ `<模块>_page_router.go`（页面）这种分法把它们放进
//     两个文件 —— 改一处忘另一处不会报错。所以下面按**功能**分节，每节接口面在前、
//     页面面在后。
//
// 中间件链：`/admin` 与编辑器根级页面组由装配层预先挂好 Session + CSRF + 权限上下文；
// `/api/page` 组走 authorizedAPI 的组链，权限点作为第二实参声明（供 check-permission-gaps.sh
// 对账）。**页面组的写动作一律按「对应接口的路径」显式挂 Casbin**：页面路径与权限点路径
// 不一致，直接按页面路径 enforce 会因权限点表里没有那条路径而拒绝所有用户（含超管）。
// 同理，批量删除挂**后台页面组**并复用单条删除的权限点 —— authorizedAPI 组按实际请求路径
// enforce，为「单条删除的加速器」单开权限点要写迁移，不值得（与商品 / 导航 / 文案同构）。
//
// pages / workbenchPages 为 nil 时只跳过对应页面注册 —— 与 rg 的既有语义同构，
// 模块装配不因缺少页面组而失败。

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/adminui"
	"go_wp/internal/builder/core"
	"go_wp/internal/middleware/builtin"
	artifactcontract "go_wp/internal/module/artifact/contract"
	blockcontract "go_wp/internal/module/block/contract"
	mediacontract "go_wp/internal/module/media/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	plugincontract "go_wp/internal/module/plugin/contract"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/permission"
)

// SetupPageRoutes 自装配 page 模块并注册全部路由。
// SiteSlotsPageTemplate 系统页面槽位页的模板名。
const SiteSlotsPageTemplate = "admin/page/site_slots.html"

// RedirectPageTemplate 重定向管理页的模板名（页面出口由页面槽带进上下文）。
const RedirectPageTemplate = "admin/page/page_redirects.html"

// PagesListPageTemplate 页面列表（后台主入口之一）。
const PagesListPageTemplate = "admin/page/pages"

// 片段出口（htmx 局部替换）：键集只有几个槽位，走 BareSlot。
// 模板名归路由层 —— 控制器只交视图，不持有模板名。
const (
	PageLangsPanelTemplate    = "fragments/page_langs"
	PageSchedulePanelTemplate = "fragments/page_schedule_panel"
	PageHistoryListTemplate   = "fragments/history_list"
)

func SetupPageRoutes(rg *permission.RouteGroup, db *gorm.DB,
	artifacts artifactcontract.ArtifactService,
	routes pubcontract.PublicationService,
	projectService projectcontract.ProjectService,
	blocks blockcontract.BlockService,
	plugins plugincontract.PluginService,
	content core.CollectionResolver,
	navigation navigationcontract.NavigationService,
	media mediacontract.MediaService,
	pages *gin.RouterGroup,
	workbenchPages *gin.RouterGroup) pagecontract.PageService {
	model := pagemodel.NewPageModel(db)
	svc := pageservice.NewService(model, artifacts, routes, projectService, blocks, plugins, content, navigation, media)
	handle := NewHandle(svc)
	adminHandle := NewPagesAdminHandle(svc, projectService, blocks, nil)

	// /api/page：接口面。页面组与编辑器页面组由装配层传入（见函数头）。
	g := rg.Group("/page", builtin.SessionAuthMiddleware())

	registerPageRoutes(g, pages, workbenchPages, handle, adminHandle, svc, projectService)

	// 保留期任务（IDX-004 / IDX-005）：历史快照收敛 + 产物 GC 定时化。
	// 此前产物 GC 只能人工调接口、修订快照完全不清理 —— 没有定时任务等于没有保留期。
	pageservice.StartPageRetentionScheduler(svc)
	// 发布回执调度（TX-009）：上次进程若崩在「已切换访问面、未写数据库」之间，
	// 收敛例程按符号链接的实际指向补齐数据库状态（或结案为未生效）。
	//
	// 调度器先跑一次**全量恢复**（等价于原先这里的裸启动恢复，只是换了个驱动源），
	// 之后按间隔兜底，并在写路径提交后由快通道即时触发 —— 不再「只有重启才收敛」，
	// 因此这里不能保留第二个「启动时跑一次」的入口（同一段实现被两个 goroutine
	// 并发重放虽幂等，仍会白白多跑一遍）。详见 service/page_publish_converge.go。
	pageservice.StartPendingReceiptConvergenceScheduler(svc)
	// 定时上下线调度（PIPE-7）：进程内 ticker（不依赖 asynq —— queue.enabled 默认 false，
	// 挂在队列上会让未启用队列的部署静默不生效）。启动首跑补上进程停机期间到点的排定。
	// 多实例下重复扫描安全：认领是一条 FOR UPDATE SKIP LOCKED 的原子语句，
	// 同一瞬间只有一个实例领到同一条排定。
	pageservice.StartPageScheduleScheduler(svc)
	return svc
}

// registerPageRoutes 声明 page 模块的全部路由。
//
// 这是本模块**唯一**的路由注册入口（文件头规则 1）—— 新增入口请加在下面对应功能的分节里，
// 不要另开 router 文件。分节按业务功能，每节内接口面在前、页面面在后（文件头规则 2）。
func registerPageRoutes(g *permission.RouteGroup, pages, workbenchPages *gin.RouterGroup,
	handle *Handle, adminHandle *pagesAdminHandle,
	svc pagecontract.PageService, projects projectcontract.ProjectService) {
	// ══ 页面列表 / 产物灾难恢复 / SEO 巡检 ══
	// 接口面都围着同一张页面列表转，由工作台 / 脚本 / 批量任务调用。
	g.POST("/create", permission.PageCreate, handle.Create)
	g.GET("/list", permission.PageList, handle.List)
	g.GET("/detail", permission.PageDetail, handle.Detail)
	g.POST("/draft/save", permission.PageDraftSave, handle.SaveDraft)
	g.GET("/revision/list", permission.PageRevisionList, handle.ListRevisions)
	g.POST("/build", permission.PageBuild, handle.Build)
	g.POST("/publish", permission.PagePublish, handle.Publish)
	g.POST("/rollback", permission.PageRollback, handle.Rollback)
	g.POST("/url/update", permission.PageURLUpdate, handle.UpdateURL)
	g.POST("/delete", permission.PageDelete, handle.Delete)
	// 灾难恢复：按产物元数据重建丢失的产物文件 + 激活面巡检。
	g.POST("/artifact/rebuild", permission.PageArtifactRebuild, handle.RebuildArtifact)
	g.GET("/publication/audit", permission.PagePublicationAudit, handle.AuditPublication)
	// 构建期 SEO 合规巡检（审计 SEO-01 前半段）：只读报告，URL / 规则 / 证据 / ArtifactHash。
	// 与 /publication/audit（访问面悬空链接）是两件事：那条问「线上链得到文件吗」，
	// 这条问「产物字节自己说的话前后一致吗」。
	g.GET("/seo/patrol", permission.PageSEOPatrol, handle.SEOPatrol)
	// 产物回收：默认 dryRun（只列候选），需显式传 dryRun=false 才实际删除。
	g.POST("/artifact/gc", permission.PageArtifactGc, handle.GarbageCollectArtifacts)
	// 页面面：运营看到的那一页（列表、新建工程、新建页面、单删 / 批删）。
	if pages == nil {
		return
	}
	pages.GET("/pages", adminui.PageAuthz("/api/page/list"),
		adminui.PageSlot(PagesListPageTemplate), adminHandle.PagesList)
	pages.POST("/pages/create", builtin.CasbinMiddlewareForPath("/api/page/create"), adminHandle.CreatePage)
	// 单条删除与批量删除复用「删除页面」权限点（/api/page/delete，迁移 151）：
	// 两者走同一个 svc.Delete —— 权限点、拒绝规则、访问面下线动作都不会分叉。
	// 批量删除不能自成一个权限点：它只是单条删除的加速器，不是另一件事。
	pages.POST("/pages/delete", builtin.CasbinMiddlewareForPath("/api/page/delete"), adminHandle.DeletePage)
	pages.POST("/pages/bulk-delete", builtin.CasbinMiddlewareForPath("/api/page/delete"), adminHandle.PagesBulkDelete)
	// 新建工程挂在页面组：入口是页面列表页的「新建站点工程」按钮。
	pages.POST("/projects/create", builtin.CasbinMiddlewareForPath("/api/project/create"), adminHandle.CreateProject)

	// ══ 修订历史 ══
	// 挂在**编辑器根级页面组**（/workbench）而不是 /admin：入口是可视化编辑器里的
	// 「历史」按钮。POST /workbench/history 只读（列出版本），/restore 会覆盖草稿，
	// 所以后者必须显式挂「保存草稿」的权限点 —— 否则任何仅登录后台的低权限用户
	// 都能覆盖任意页面草稿。
	if workbenchPages != nil {
		workbenchPages.POST("/workbench/history",
			adminui.BareSlot(PageHistoryListTemplate), adminHandle.HistoryPanel)
		workbenchPages.POST("/workbench/history/restore", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), adminHandle.HistoryRestore)
	}

	// ══ 页面级语言排除（只有页面面）══
	// 没有接口通道：唯一使用者是运营在页面列表行内点开的那块面板，面板本身就是
	// HTMX 片段（服务端渲染），不存在「脚本要批量排除某语言」的场景 —— 真出现时它该走的是
	// 「发布范围」那条批量入口，而不是绕过界面直接改排除列。
	//
	// 权限点：排除会**真的下线该语言的产物**（pageservice.ExcludePageLang 在同一事务里
	// 清发布 / 暂存 / 路由 / 计划），因此复用「发布页面」；恢复只改产出范围（不自动重新发布），
	// 同一条 —— 两者都是「这一页发不发这种语言」的同一件事，不该拆成两个权限点。
	// 面板是只读展示，复用「页面详情」。
	pages.GET("/page-langs/panel", builtin.CasbinMiddlewareForPath("/api/page/detail"),
		adminui.BareSlot(PageLangsPanelTemplate), adminHandle.PageLangsPanel)
	pages.POST("/page-langs/exclude", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.PageLangExclude)
	pages.POST("/page-langs/restore", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.PageLangRestore)
	// 显式重新发布（V4）：恢复排除只解除限制、不自动上线 —— 译好后要真的回到线上，
	// 需要一个一次点击的入口（同步 Build + Publish，结果当场可见）。权限点同上。
	pages.POST("/page-langs/republish", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.PageLangRepublish)

	// ══ 定时上下线（PIPE-7）══
	//   · JSON 接口（/api/page/schedule/{set,cancel,list}）：给工作台 / 脚本 / 后续的
	//     批量排定用。错误经 pageErrorStatus / pageErrorMessage 分类，状态码与文案与其它
	//     page 接口同源。
	//   · 后台面板（/admin/page-schedules/{panel,set,cancel}）：HTMX 片段，
	//     表单是 form-urlencoded（原生表单 + 隐藏 csrf_token 域），成功后重新渲染面板片段。
	//     为什么不让 HTMX 直接打 JSON 接口：HTMX 的表单 POST 是 form-encoded，
	//     而 JSON 接口只收 JSON —— 要么引入 json-enc 扩展（新增前端依赖），
	//     要么在接口里做双形态绑定（两套解析路径，容易只测到一条）。
	//
	// 到点只切访问面指针、不重编译；排定后草稿被改则硬失败。
	// 三条接口路由的权限点是新增的（page:schedule_*）—— authorizedAPI 组按实际路径 enforce，
	// 权限点表里没有条目会让含超管在内全员 403（见 AGENTS.md §数据库）。
	// 面板的鉴权显式复用这组接口的路径（页面路径与权限点路径不一致，直接按页面路径 enforce 会全员 403）。
	g.POST("/schedule/set", permission.PageScheduleSet, handle.SetSchedule)
	g.POST("/schedule/cancel", permission.PageScheduleCancel, handle.CancelSchedule)
	g.GET("/schedule/list", permission.PageScheduleList, handle.ListSchedules)
	pages.GET("/page-schedules/panel", builtin.CasbinMiddlewareForPath("/api/page/schedule/list"),
		adminui.BareSlot(PageSchedulePanelTemplate), adminHandle.SchedulePanel)
	pages.POST("/page-schedules/set", builtin.CasbinMiddlewareForPath("/api/page/schedule/set"), adminHandle.ScheduleSet)
	pages.POST("/page-schedules/cancel", builtin.CasbinMiddlewareForPath("/api/page/schedule/cancel"), adminHandle.ScheduleCancel)

	// ══ 页面翻译工作台（多语言 P5c，docs/06-D §7.8，只有页面面）══
	// 入口是页面列表行内的「多语言」按钮，整页服务端渲染 + HTMX 片段刷新。
	// 工作台做的事（录译文、看复用提示、看完成度）本来就只有人在做，
	// 没有「脚本批量录译文」的场景 —— 真要做也该先有候选清单接口，而不是绕过界面写 sys_translation。
	//
	// 鉴权：读复用「列出页面」，写复用「保存草稿」—— 保存会写 sys_translation（engine=manual）
	// 并触发全站标记待重建，与保存草稿同一量级的影响面。
	pages.GET("/pages/translations", adminui.PageAuthz("/api/page/list"), adminHandle.PageTranslations)
	pages.POST("/pages/translations/save", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), adminHandle.SavePageTranslations)

	// ══ 缺译报告（U2，只有页面面）══
	// 与站点级准入（U1，project.SaveLocales 的界面词条门槛）的分工：
	// **U1 拦「这个语言整体没准备好」，U2 处理「语言准备好了、但某些页面的内容没译」**。
	//
	// 报告是「按 页面 × 语言 列出内容缺译」的巡检视图，操作列的
	// 「取消该语言」直接调 ExcludePageLang（不另写下线逻辑）——
	// 取消 = 下线该语言产物 + 清发布 / 暂存 / 路由 / 计划（同一事务）+ 写排除列，
	// 那套语义以及它与切换器 / hreflang / sitemap 的一致性已经在语言排除那一处验证过。
	//
	// 挂**后台页面组**（Session + CSRF 已具备），写操作显式复用「发布页面」权限点。
	pages.GET("/page-translation-misses", builtin.CasbinMiddlewareForPath("/api/page/list"), adminHandle.TranslationMissesPage)
	pages.POST("/page-translation-misses/cancel", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.TranslationMissCancel)

	// ══ 重定向管理（审计 SEO-025）══
	// 改 URL 留下的旧路径 301 此前只能生效、不能查看与清理。接口面给工作台与脚本，
	// 页面面是 /admin/pages/redirects 那一页。
	//
	// 页面路由挂在 `/api/page/redirect*` 上而不是 `/admin/*`：page 模块只拿到
	// authorizedAPI 这一个装配好的组，挂这里可让 Session / CSRF / Casbin 三层链与接口
	// 完全一致（不需要改动 routes.go）。代价是 URL 少一层「后台感」。
	// 这一条渲染**整页**（admin/page/page_redirects.html），所以挂 PageSlot：
	// 路由挂在 /api 组只是为了让 Session/CSRF/Casbin 三层链与接口一致（见上方注释），
	// 与"它是不是页面出口"无关。
	g.GET("/redirect",
		permission.PageRedirectView,
		adminui.PageSlot(RedirectPageTemplate),
		handle.RedirectPage)
	g.POST("/redirect/create", permission.PageRedirectCreate, handle.RedirectCreate)
	g.POST("/redirect/delete", permission.PageRedirectDelete, handle.RedirectDelete)
	g.POST("/redirect/merge", permission.PageRedirectMerge, handle.RedirectMerge)
	pages.POST("/page-redirects/bulk-delete", builtin.CasbinMiddlewareForPath("/api/page/redirect/delete"), handle.RedirectBulkDelete)

	// ══ 系统页面槽位（BIZ-1）══
	// 槽位把「结算页是哪一页」这类事实固定下来，供链接生成与跳转使用。
	// 接口面（/api/page/site-slot/{list,bind,unbind}）给构建期与其它模块；
	// 页面面（/admin/site-slots）是运营配置它那一页 —— 侧栏入口是 sys_menus 里
	// 「内容」分组下的「系统页面」（迁移 140 落行，224 收口归位）。
	//
	// 两侧用不同的 handler 是对的：接口面回 JSON、页面面回整页 HTML，
	// 但**权限点同源**（页面侧显式指向接口路径），改一处不会漏另一处。
	g.GET("/site-slot/list", permission.PageSiteSlotList, handle.ListSiteSlots)
	g.POST("/site-slot/bind", permission.PageSiteSlotBind, handle.BindSiteSlot)
	g.POST("/site-slot/unbind", permission.PageSiteSlotUnbind, handle.UnbindSiteSlot)
	if svc == nil || projects == nil {
		return
	}
	siteSlotPage := NewSiteSlotPageHandle(svc, projects)
	pages.GET("/site-slots",
		adminui.PageAuthz("/api/page/list"),
		adminui.PageSlot(SiteSlotsPageTemplate),
		siteSlotPage.SiteSlotsPage)
	pages.POST("/site-slots/bind", builtin.CasbinMiddlewareForPath("/api/page/site-slot/bind"), siteSlotPage.SiteSlotBind)
	pages.POST("/site-slots/unbind", builtin.CasbinMiddlewareForPath("/api/page/site-slot/unbind"), siteSlotPage.SiteSlotUnbind)
}

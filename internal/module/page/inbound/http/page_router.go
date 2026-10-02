package pagehttp

import (
	"go_wp/internal/middleware/builtin"
	"go_wp/internal/permission"

	blockcontract "go_wp/internal/module/block/contract"

	"go_wp/internal/builder/core"
	mediacontract "go_wp/internal/module/media/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	plugincontract "go_wp/internal/module/plugin/contract"
	projectcontract "go_wp/internal/module/project/contract"

	artifactcontract "go_wp/internal/module/artifact/contract"
	pubcontract "go_wp/internal/module/publication/contract"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupPageRoutes 自装配 page 模块并注册草稿、修订与发布路由。
//
// pages 是装配层传入的 /admin 后台页面组（已挂 Session + CSRF + 权限上下文，见
// assembly.go 的 adminPages）；workbenchPages 是根级前缀的编辑器页面组（同中间件），
// 供 /workbench/history* 两条修订历史路由挂载。后台页面与 API 在这一处同时装配。
// 任一页面组为 nil 时只跳过对应页面注册 —— 与 rg 的既有语义同构，模块装配不因
// 缺少页面组而失败。
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

	g := rg.Group("/page", builtin.SessionAuthMiddleware())
	g.POST("/create", permission.PageCreate, handle.Create)
	g.GET("/list", permission.PageList, handle.List)
	g.GET("/detail", permission.PageDetail, handle.Detail)
	g.POST("/draft/save", permission.PageDraftSave, handle.SaveDraft)
	g.GET("/revision/list", permission.PageRevisionList, handle.ListRevisions)
	g.POST("/build", permission.PageBuild, handle.Build)
	g.POST("/publish", permission.PagePublish, handle.Publish)
	g.POST("/rollback", permission.PageRollback, handle.Rollback)
	g.POST("/url/update", permission.PageURLUpdate, handle.UpdateURL)
	// 重定向管理（审计 SEO-025）：改 URL 留下的旧路径 301 此前只能生效、不能查看与清理。
	// 页面路由同样挂在本组：page 模块只拿到 authorizedAPI 这一个装配好的组，
	// 挂这里可让 Session / CSRF / Casbin 三层链与 API 完全一致（不改 routes.go）。
	g.GET("/redirect", permission.PageRedirectView, handle.RedirectPage)
	g.POST("/redirect/create", permission.PageRedirectCreate, handle.RedirectCreate)
	g.POST("/redirect/delete", permission.PageRedirectDelete, handle.RedirectDelete)
	g.POST("/redirect/merge", permission.PageRedirectMerge, handle.RedirectMerge)
	// 缺译报告（U2）：按 页面 × 语言 列出内容缺译，操作列直接调 ExcludePageLang（不另写下线逻辑）。
	// 权限点复用：读用 page:list；写（取消该语言）用 page:publish —— 取消会下线产物，与「撤下」是同一件事。
	g.GET("/translation-misses", permission.PageList, handle.TranslationMissesPage)
	g.POST("/translation-misses/cancel", permission.PagePublish, handle.TranslationMissCancel)
	// 系统页面槽位（BIZ-1）：把「结算页是哪一页」这类事实固定下来，供链接生成与跳转使用。
	g.GET("/site-slot/list", permission.PageSiteSlotList, handle.ListSiteSlots)
	g.POST("/site-slot/bind", permission.PageSiteSlotBind, handle.BindSiteSlot)
	g.POST("/site-slot/unbind", permission.PageSiteSlotUnbind, handle.UnbindSiteSlot)
	g.POST("/delete", permission.PageDelete, handle.Delete)
	// 后台页面（/admin/site-slots）：壳层与权限点见 router_site_slot.go。
	// 页面不在 /api 下 —— /api 那组的三层链是给接口用的，后台页面组由装配层预先挂好。
	setupSiteSlotPageRoutes(pages, svc, projectService)

	// 页面列表与翻译工作台（从 dashboard 回迁）：/admin/pages、/admin/page/translations。
	// 恢复复用 pagesAdminHandle（见 pages_handle.go）；写操作复用对应 API 权限点做
	// Casbin 鉴权（页面路径与权限点路径不一致，直接按页面路径 enforce 会因权限点表
	// 无此路径而拒绝所有用户）。
	adminHandle := NewPagesAdminHandle(svc, projectService, blocks, nil)
	if pages != nil {
		pages.GET("/pages", adminHandle.PagesList)
		pages.POST("/pages/create", builtin.CasbinMiddlewareForPath("/api/page/create"), adminHandle.CreatePage)
		// 单条删除与批量删除复用「删除页面」权限点（/api/page/delete，迁移 151）：
		// 两者走同一个 svc.Delete —— 权限点、拒绝规则、访问面下线动作都不会分叉。
		// 批量删除不能自成一个权限点：它只是单条删除的加速器，不是另一件事。
		pages.POST("/pages/delete", builtin.CasbinMiddlewareForPath("/api/page/delete"), adminHandle.DeletePage)
		pages.POST("/pages/bulk-delete", builtin.CasbinMiddlewareForPath("/api/page/delete"), adminHandle.PagesBulkDelete)
		// 重定向的批量删除挂**后台页面组**、而不是 /api 组：
		// authorizedAPI 组统一按实际请求路径 enforce（scripts/check-permission-gaps.sh 专盯这条），
		// 新路径在 sys_permission 里没有条目 → 含超管在内一律 403；而补一条权限点必须写迁移，
		// 批量删除只是单条删除的加速器，不值得为它单开权限点。
		// 于是与商品 / 导航 / 文案三个域的批量端点同构：挂 /admin 组（Session + CSRF 已具备）
		// + 显式复用单条删除的权限点路径 /api/page/redirect/delete。
		pages.POST("/page-redirects/bulk-delete", builtin.CasbinMiddlewareForPath("/api/page/redirect/delete"), handle.RedirectBulkDelete)
		// 翻译工作台（多语言 P5c，docs/06-D §7.8）：入口在页面列表行内「多语言」按钮。
		// 保存写 sys_translation（engine=manual）并触发全站标记待重建，鉴权复用「保存草稿」权限点。
		pages.GET("/page/translations", adminHandle.PageTranslations)
		// 定时上下线的面板与表单（PIPE-7）：面板是 HTMX 片段（列表页行内「定时」按钮的落点），
		// 两个 POST 是原生表单（form-urlencoded + 隐藏 csrf_token 域），鉴权复用 API 的权限点路径
		// —— 页面路径与权限点路径不一致，直接按页面路径 enforce 会因权限点表无此路径而拒绝所有用户
		// （与 /pages/page-redirects/bulk-delete 同一手法）。
		pages.GET("/page-schedules/panel", builtin.CasbinMiddlewareForPath("/api/page/schedule/list"), adminHandle.SchedulePanel)
		pages.POST("/page-schedules/set", builtin.CasbinMiddlewareForPath("/api/page/schedule/set"), adminHandle.ScheduleSet)
		pages.POST("/page-schedules/cancel", builtin.CasbinMiddlewareForPath("/api/page/schedule/cancel"), adminHandle.ScheduleCancel)
		pages.POST("/page/translations/save", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), adminHandle.SavePageTranslations)
		// 页面级语言排除（迁移 491）：面板展示本页各语言的产出范围（默认语言 / 已发布 /
		// 已排除），可排除与恢复。**排除会真的下线该语言产物**（pageservice.ExcludePageLang
		// 在同一事务里清发布/暂存/路由/计划），因此鉴权复用「发布页面」权限点；
		// 恢复只改产出范围（不自动重新发布），鉴权同一条 —— 两者都是「这一页发不发这种语言」
		// 的同一件事，不该拆成两个权限点。
		pages.GET("/page-langs/panel", builtin.CasbinMiddlewareForPath("/api/page/detail"), adminHandle.PageLangsPanel)
		pages.POST("/page-langs/exclude", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.PageLangExclude)
		pages.POST("/page-langs/restore", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.PageLangRestore)
		// 显式重新发布（V4）：恢复排除只解除限制、不自动上线 —— 译好后要真的回到线上，
		// 需要一个一次点击的入口（同步 Build + Publish，结果当场可见）。权限点同上。
		pages.POST("/page-langs/republish", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.PageLangRepublish)
		pages.POST("/projects/create", builtin.CasbinMiddlewareForPath("/api/project/create"), adminHandle.CreateProject)
	}
	// 修订历史列表与恢复（HTMX 化，docs/09 §3）：挂编辑器根级页面组。
	// 恢复修订会覆盖页面草稿（写操作），权限点复用「保存草稿」——
	// 否则任何仅登录后台的低权限用户都能覆盖任意页面草稿。
	if workbenchPages != nil {
		workbenchPages.POST("/workbench/history", adminHandle.HistoryPanel)
		workbenchPages.POST("/workbench/history/restore", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), adminHandle.HistoryRestore)
	}

	// 灾难恢复：按产物元数据重建丢失的产物文件 + 激活面巡检。
	g.POST("/artifact/rebuild", permission.PageArtifactRebuild, handle.RebuildArtifact)
	g.GET("/publication/audit", permission.PagePublicationAudit, handle.AuditPublication)
	// 构建期 SEO 合规巡检（审计 SEO-01 前半段）：只读报告，URL / 规则 / 证据 / ArtifactHash。
	// 与 /publication/audit（访问面悬空链接）是两件事：那条问「线上链得到文件吗」，
	// 这条问「产物字节自己说的话前后一致吗」。
	g.GET("/seo/patrol", permission.PageSEOPatrol, handle.SEOPatrol)
	// 产物回收：默认 dryRun（只列候选），需显式传 dryRun=false 才实际删除。
	g.POST("/artifact/gc", permission.PageArtifactGc, handle.GarbageCollectArtifacts)
	// 定时上下线（PIPE-7）：到点只切指针不重编译；排定后草稿被改则硬失败。
	// 三条路由的权限点是新增的（page:schedule_*）—— authorizedAPI 组按实际路径 enforce，
	// 权限点表里没有条目会让含超管在内全员 403（见 AGENTS.md §数据库）。
	g.POST("/schedule/set", permission.PageScheduleSet, handle.SetSchedule)
	g.POST("/schedule/cancel", permission.PageScheduleCancel, handle.CancelSchedule)
	g.GET("/schedule/list", permission.PageScheduleList, handle.ListSchedules)
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

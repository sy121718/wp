package pagehttp

// 页面挂在装配层传入的 /admin 组上：该组已有 Session + CSRF + 权限上下文中间件
// （见 internal/routers/assembly.go 的 adminPages）。写动作额外按**对应 API 的路径**
// 走 Casbin 权限点 —— 与 /api/page/site-slot/* 的权限点完全同源，一个字符都不改。
// pages 为 nil 时跳过注册：模块装配不因缺少页面组而失败，与 rg 的既有语义同构。

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/artifact/contract"
	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/media/contract"
	"go_wp/internal/module/navigation/contract"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/page/model"
	"go_wp/internal/module/page/service"
	"go_wp/internal/module/plugin/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/publication/contract"
	"go_wp/internal/permission"
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
	// 系统页面槽位（BIZ-1）：把「结算页是哪一页」这类事实固定下来，供链接生成与跳转使用。
	g.GET("/site-slot/list", permission.PageSiteSlotList, handle.ListSiteSlots)
	g.POST("/site-slot/bind", permission.PageSiteSlotBind, handle.BindSiteSlot)
	g.POST("/site-slot/unbind", permission.PageSiteSlotUnbind, handle.UnbindSiteSlot)
	g.POST("/delete", permission.PageDelete, handle.Delete)
	// 后台页面（/admin/site-slots）：壳层与权限点见 router_site_slot.go。
	// 页面不在 /api 下 —— /api 那组的三层链是给接口用的，后台页面组由装配层预先挂好。
	setupSiteSlotPageRoutes(pages, svc, projectService)
	// 后台页面与编辑器修订历史：注册在 page_page_router.go（同一入口调用，装配顺序不变）。
	SetupPageAdminPages(pages, workbenchPages, svc, projectService, blocks, handle)

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

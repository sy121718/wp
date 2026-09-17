package pagehttp

import (
	"context"
	"time"

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
		// 翻译工作台（多语言 P5c，docs/06-D §7.8）：入口在页面列表行内「多语言」按钮。
		// 保存写 sys_translation（engine=manual）并触发全站标记待重建，鉴权复用「保存草稿」权限点。
		pages.GET("/page/translations", adminHandle.PageTranslations)
		pages.POST("/page/translations/save", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), adminHandle.SavePageTranslations)
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
	// 产物回收：默认 dryRun（只列候选），需显式传 dryRun=false 才实际删除。
	g.POST("/artifact/gc", permission.PageArtifactGc, handle.GarbageCollectArtifacts)
	// 保留期任务（IDX-004 / IDX-005）：历史快照收敛 + 产物 GC 定时化。
	// 此前产物 GC 只能人工调接口、修订快照完全不清理 —— 没有定时任务等于没有保留期。
	pageservice.StartPageRetentionScheduler(svc)
	// 发布回执恢复（TX-009）：上次进程若崩在「已切换访问面、未写数据库」之间，
	// 这里按符号链接的实际指向补齐数据库状态（或结案为未生效）。
	// 异步执行：恢复要读文件系统，不该拖住路由装配。
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_, _, _ = svc.RecoverPendingPublications(ctx)
	}()
	return svc
}

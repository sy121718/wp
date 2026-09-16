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

	"gorm.io/gorm"
)

// SetupPageRoutes 自装配 page 模块并注册草稿、修订与发布路由。
func SetupPageRoutes(rg *permission.RouteGroup, db *gorm.DB,
	artifacts artifactcontract.ArtifactService,
	routes pubcontract.PublicationService,
	projectService projectcontract.ProjectService,
	blocks blockcontract.BlockService,
	plugins plugincontract.PluginService,
	content core.CollectionResolver,
	navigation navigationcontract.NavigationService,
	media mediacontract.MediaService) pagecontract.PageService {
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

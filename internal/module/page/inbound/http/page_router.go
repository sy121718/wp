package pagehttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	artifactcontract "go_wp/internal/module/artifact/contract"
	blockcontract "go_wp/internal/module/block/contract"
	mediacontract "go_wp/internal/module/media/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	plugincontract "go_wp/internal/module/plugin/contract"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/pkg/builder/core"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

const RedirectPageTemplate = "admin/page/page_redirects.html"

const PagesListPageTemplate = "admin/page/pages"

const (
	PageHistoryListTemplate = "fragments/history_list"

	PageTranslationsTemplate = "admin/page/page_translations"

	PageTranslationMissesTemplate = "admin/page/page_translation_misses.html"
)

func SetupPageRoutes(rg *mw.RouteGroup, db *gorm.DB,
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

	g := rg.Group("/page", builtin.SessionAuthMiddleware())

	registerPageRoutes(g, pages, workbenchPages, handle, adminHandle, svc, projectService)

	pageservice.StartPageRetentionScheduler(svc)

	pageservice.StartPendingReceiptConvergenceScheduler(svc)
	return svc
}

func registerPageRoutes(g *mw.RouteGroup, pages, workbenchPages *gin.RouterGroup,
	handle *Handle, adminHandle *pagesAdminHandle,
	svc pagecontract.PageService, projects projectcontract.ProjectService) {

	g.POST("/create", pageenums.PageCreate, handle.Create)
	g.GET("/list", pageenums.PageList, handle.List)
	g.GET("/detail", pageenums.PageDetail, handle.Detail)
	g.POST("/draft/save", pageenums.PageDraftSave, handle.SaveDraft)
	// 译文保存是 JSON 写出口：写基座只对接 /api/ 端点（data-write-url 的契约）。
	g.POST("/translations/save", pageenums.PageTranslationsSave, pageview.HTMLFragment, adminHandle.SavePageTranslations)
	g.GET("/revision/list", pageenums.PageRevisionList, handle.ListRevisions)
	g.POST("/build", pageenums.PageBuild, handle.Build)
	g.POST("/publish", pageenums.PagePublish, handle.Publish)
	g.POST("/rollback", pageenums.PageRollback, handle.Rollback)
	g.POST("/url/update", pageenums.PageURLUpdate, handle.UpdateURL)
	g.POST("/delete", pageenums.PageDelete, handle.Delete)

	g.POST("/artifact/rebuild", pageenums.PageArtifactRebuild, handle.RebuildArtifact)
	g.GET("/publication/audit", pageenums.PagePublicationAudit, handle.AuditPublication)

	g.GET("/seo/patrol", pageenums.PageSEOPatrol, handle.SEOPatrol)

	g.POST("/artifact/gc", pageenums.PageArtifactGc, handle.GarbageCollectArtifacts)

	if pages == nil {
		return
	}
	pages.GET("/pages", adminui.PageAuthz("/api/page/list"),
		pageview.UseRenderer(adminui.RenderPage), adminHandle.PagesList)
	pages.POST("/pages/create", builtin.CasbinMiddlewareForPath("/api/page/create"), pageview.HTMLFragment, adminHandle.CreatePage)

	pages.POST("/pages/delete", builtin.CasbinMiddlewareForPath("/api/page/delete"), pageview.HTMLFragment, adminHandle.DeletePage)
	pages.POST("/pages/bulk-delete", builtin.CasbinMiddlewareForPath("/api/page/delete"), pageview.HTMLFragment, adminHandle.PagesBulkDelete)

	pages.POST("/projects/create", builtin.CasbinMiddlewareForPath("/api/project/create"), pageview.HTMLFragment, adminHandle.CreateProject)

	if workbenchPages != nil {
		workbenchPages.POST("/workbench/history",
			pageview.UseRenderer(adminui.RenderBare), adminHandle.HistoryPanel)
		workbenchPages.POST("/workbench/history/restore", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), adminHandle.HistoryRestore)
	}

	pages.POST("/page-langs/exclude", builtin.CasbinMiddlewareForPath("/api/page/publish"), pageview.HTMLFragment, adminHandle.PageLangExclude)
	pages.POST("/page-langs/restore", builtin.CasbinMiddlewareForPath("/api/page/publish"), pageview.HTMLFragment, adminHandle.PageLangRestore)

	pages.POST("/page-langs/republish", builtin.CasbinMiddlewareForPath("/api/page/publish"), pageview.HTMLFragment, adminHandle.PageLangRepublish)

	pages.GET("/pages/translations", adminui.PageAuthz("/api/page/list"),
		pageview.UseRenderer(adminui.RenderPage), adminHandle.PageTranslations)

	pages.GET("/page-translation-misses", builtin.CasbinMiddlewareForPath("/api/page/list"),
		pageview.UseRenderer(adminui.RenderPage), adminHandle.TranslationMissesPage)
	pages.POST("/page-translation-misses/cancel", builtin.CasbinMiddlewareForPath("/api/page/publish"), pageview.HTMLFragment, adminHandle.TranslationMissCancel)

	g.GET("/redirect",
		pageenums.PageRedirectView,
		pageview.UseRenderer(adminui.RenderPage),
		handle.RedirectPage)
	g.POST("/redirect/create", pageenums.PageRedirectCreate, pageview.HTMLFragment, handle.RedirectCreate)
	g.POST("/redirect/delete", pageenums.PageRedirectDelete, pageview.HTMLFragment, handle.RedirectDelete)
	g.POST("/redirect/merge", pageenums.PageRedirectMerge, pageview.HTMLFragment, handle.RedirectMerge)
	pages.POST("/page-redirects/bulk-delete", builtin.CasbinMiddlewareForPath("/api/page/redirect/delete"), pageview.HTMLFragment, handle.RedirectBulkDelete)

}

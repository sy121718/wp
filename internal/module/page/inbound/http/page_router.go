package pagehttp

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
	pageview "go_wp/pkg/view"
)

const RedirectPageTemplate = "admin/page/page_redirects.html"

const PagesListPageTemplate = "admin/page/pages"

const (
	PageHistoryListTemplate = "fragments/history_list"

	PageTranslationsTemplate = "admin/page/page_translations"

	PageTranslationMissesTemplate = "admin/page/page_translation_misses.html"

	PageTranslationErrorsTemplate = "fragments/translation_errors.html"
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

	g := rg.Group("/page", builtin.SessionAuthMiddleware())

	registerPageRoutes(g, pages, workbenchPages, handle, adminHandle, svc, projectService)

	pageservice.StartPageRetentionScheduler(svc)

	pageservice.StartPendingReceiptConvergenceScheduler(svc)
	return svc
}

func registerPageRoutes(g *permission.RouteGroup, pages, workbenchPages *gin.RouterGroup,
	handle *Handle, adminHandle *pagesAdminHandle,
	svc pagecontract.PageService, projects projectcontract.ProjectService) {

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

	g.POST("/artifact/rebuild", permission.PageArtifactRebuild, handle.RebuildArtifact)
	g.GET("/publication/audit", permission.PagePublicationAudit, handle.AuditPublication)

	g.GET("/seo/patrol", permission.PageSEOPatrol, handle.SEOPatrol)

	g.POST("/artifact/gc", permission.PageArtifactGc, handle.GarbageCollectArtifacts)

	if pages == nil {
		return
	}
	pages.GET("/pages", adminui.PageAuthz("/api/page/list"),
		pageview.UseRenderer(adminui.RenderPage), adminHandle.PagesList)
	pages.POST("/pages/create", builtin.CasbinMiddlewareForPath("/api/page/create"), adminHandle.CreatePage)

	pages.POST("/pages/delete", builtin.CasbinMiddlewareForPath("/api/page/delete"), adminHandle.DeletePage)
	pages.POST("/pages/bulk-delete", builtin.CasbinMiddlewareForPath("/api/page/delete"), adminHandle.PagesBulkDelete)

	pages.POST("/projects/create", builtin.CasbinMiddlewareForPath("/api/project/create"), adminHandle.CreateProject)

	if workbenchPages != nil {
		workbenchPages.POST("/workbench/history",
			pageview.UseRenderer(adminui.RenderBare), adminHandle.HistoryPanel)
		workbenchPages.POST("/workbench/history/restore", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), adminHandle.HistoryRestore)
	}

	pages.POST("/page-langs/exclude", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.PageLangExclude)
	pages.POST("/page-langs/restore", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.PageLangRestore)

	pages.POST("/page-langs/republish", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.PageLangRepublish)

	pages.GET("/pages/translations", adminui.PageAuthz("/api/page/list"),
		pageview.UseRenderer(adminui.RenderPage), adminHandle.PageTranslations)
	pages.POST("/pages/translations/save", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), adminHandle.SavePageTranslations)

	pages.GET("/page-translation-misses", builtin.CasbinMiddlewareForPath("/api/page/list"),
		pageview.UseRenderer(adminui.RenderPage), adminHandle.TranslationMissesPage)
	pages.POST("/page-translation-misses/cancel", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.TranslationMissCancel)

	g.GET("/redirect",
		permission.PageRedirectView,
		pageview.UseRenderer(adminui.RenderPage),
		handle.RedirectPage)
	g.POST("/redirect/create", permission.PageRedirectCreate, handle.RedirectCreate)
	g.POST("/redirect/delete", permission.PageRedirectDelete, handle.RedirectDelete)
	g.POST("/redirect/merge", permission.PageRedirectMerge, handle.RedirectMerge)
	pages.POST("/page-redirects/bulk-delete", builtin.CasbinMiddlewareForPath("/api/page/redirect/delete"), handle.RedirectBulkDelete)

}

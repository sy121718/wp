package pagehttp

import (
	"net/http"

	"github.com/gin-gonic/gin"
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
	"gorm.io/gorm"
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

	registerPageRoutes(rg.Registry(), g, pages, workbenchPages, handle, adminHandle, svc, projectService)

	pageservice.StartPageRetentionScheduler(svc)

	pageservice.StartPendingReceiptConvergenceScheduler(svc)
	return svc
}

func registerPageRoutes(reg *mw.Registry, g *mw.RouteGroup, pages, workbenchPages *gin.RouterGroup,
	handle *Handle, adminHandle *pagesAdminHandle,
	svc pagecontract.PageService, projects projectcontract.ProjectService) {

	g.POST("/create", pageenums.PageCreate, handle.Create)
	g.GET("/list", pageenums.PageList, handle.List)
	g.GET("/detail", pageenums.PageDetail, handle.Detail)
	g.POST("/draft/save", pageenums.PageDraftSave, handle.SaveDraft)
	// 译文保存是 JSON 写出口：写基座只对接 /api/ 端点（data-write-url 的契约）。
	g.POST("/translations/save", pageenums.PageTranslationsSave, pageview.HTMLFragment, pageview.UseRenderer(pageview.RenderBare), adminHandle.SavePageTranslations)
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
	pages.GET("/pages", mw.PageAuthz("/api/page/list"),
		pageview.UseRenderer(pageview.RenderPage), adminHandle.PagesList)
	mw.DeclarePageGetOn(reg, "/admin/pages", "/api/page/list")
	pages.POST("/pages/create", builtin.CasbinMiddlewareForPath("/api/page/create"), pageview.HTMLFragment, adminHandle.CreatePage)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/pages/create", "/api/page/create", http.MethodPost)
	}

	pages.POST("/pages/delete", builtin.CasbinMiddlewareForPath("/api/page/delete"), pageview.HTMLFragment, adminHandle.DeletePage)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/pages/delete", "/api/page/delete", http.MethodPost)
	}
	pages.POST("/pages/bulk-delete", builtin.CasbinMiddlewareForPath("/api/page/delete"), pageview.HTMLFragment, adminHandle.PagesBulkDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/pages/bulk-delete", "/api/page/delete", http.MethodPost)
	}

	pages.POST("/projects/create", builtin.CasbinMiddlewareForPath("/api/project/create"), pageview.HTMLFragment, adminHandle.CreateProject)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/projects/create", "/api/project/create", http.MethodPost)
	}

	if workbenchPages != nil {
		workbenchPages.POST("/workbench/history", builtin.CasbinMiddlewareForPath("/api/page/draft/save"),
			pageview.UseRenderer(pageview.RenderBare), adminHandle.HistoryPanel)
		if reg != nil {
			reg.DeclarePageWrite(http.MethodPost, "/workbench/history", "/api/page/draft/save", http.MethodPost)
		}
		workbenchPages.POST("/workbench/history/restore", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), adminHandle.HistoryRestore)
		if reg != nil {
			reg.DeclarePageWrite(http.MethodPost, "/workbench/history/restore", "/api/page/draft/save", http.MethodPost)
		}
	}

	pages.POST("/page-langs/exclude", builtin.CasbinMiddlewareForPath("/api/page/publish"), pageview.HTMLFragment, adminHandle.PageLangExclude)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/page-langs/exclude", "/api/page/publish", http.MethodPost)
	}
	pages.POST("/page-langs/restore", builtin.CasbinMiddlewareForPath("/api/page/publish"), pageview.HTMLFragment, adminHandle.PageLangRestore)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/page-langs/restore", "/api/page/publish", http.MethodPost)
	}

	pages.POST("/page-langs/republish", builtin.CasbinMiddlewareForPath("/api/page/publish"), pageview.HTMLFragment, adminHandle.PageLangRepublish)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/page-langs/republish", "/api/page/publish", http.MethodPost)
	}

	pages.GET("/pages/translations", mw.PageAuthz("/api/page/list"),
		pageview.UseRenderer(pageview.RenderPage), adminHandle.PageTranslations)
	mw.DeclarePageGetOn(reg, "/admin/pages/translations", "/api/page/list")

	pages.GET("/page-translation-misses", builtin.CasbinMiddlewareForPath("/api/page/list"),
		pageview.UseRenderer(pageview.RenderPage), adminHandle.TranslationMissesPage)
	mw.DeclarePageGetOn(reg, "/admin/page-translation-misses", "/api/page/list")
	pages.POST("/page-translation-misses/cancel", builtin.CasbinMiddlewareForPath("/api/page/publish"), pageview.HTMLFragment, adminHandle.TranslationMissCancel)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/page-translation-misses/cancel", "/api/page/publish", http.MethodPost)
	}

	g.GET("/redirect",
		pageenums.PageRedirectView,
		pageview.UseRenderer(pageview.RenderPage),
		handle.RedirectPage)
	g.POST("/redirect/create", pageenums.PageRedirectCreate, pageview.HTMLFragment, handle.RedirectCreate)
	g.POST("/redirect/delete", pageenums.PageRedirectDelete, pageview.HTMLFragment, handle.RedirectDelete)
	g.POST("/redirect/merge", pageenums.PageRedirectMerge, pageview.HTMLFragment, handle.RedirectMerge)
	pages.POST("/page-redirects/bulk-delete", builtin.CasbinMiddlewareForPath("/api/page/redirect/delete"), pageview.HTMLFragment, handle.RedirectBulkDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/page-redirects/bulk-delete", "/api/page/redirect/delete", http.MethodPost)
	}

}

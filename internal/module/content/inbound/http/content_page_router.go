package contenthttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

const ContentPreviewNoticeTemplate = "admin/preview_notice.html"

const ArticlesPageTemplate = "admin/content/articles.html"

const ArticleNewPageTemplate = "admin/content/article_new.html"

const ArticleTranslationsTemplate = "admin/content/article_translations.html"

const ArticleEditPageTemplate = "admin/content/article_edit.html"

const ArticleCreatedTemplate = "admin/content/article_created.html"

const ArticleImportedTemplate = "admin/content/article_imported.html"

const (
	SeoScoreFragmentTemplate      = "fragments/seo_score"
	ArticleImportFragmentTemplate = "fragments/article_import"

	ArticleSeoDrawerTemplate   = "admin/content/article_seo_drawer.html"
	ArticleStaleDrawerTemplate = "admin/partials/stale_pages_drawer.html"
)

func SetupContentPages(reg *mw.Registry, pages *gin.RouterGroup, contents contentcontract.ContentService,
	projects projectcontract.ProjectService, templates contenttemplatecontract.ContentTemplateService,
	pageSvc pagecontract.PageService, instances presentationcontract.PresentationService) {
	if pages == nil {
		return
	}

	articlePages := NewArticlePageHandle(contents, projects, templates, instances, pageSvc)

	if loc, ok := any(instances).(presentationcontract.PublishedEntityLocator); ok {
		articlePages.SetArticleLinkLocator(loc)
	}
	pages.GET("/articles",
		mw.PageAuthz("/api/content/list"),
		pageview.UseRenderer(pageview.RenderPage),
		articlePages.ArticlesPage)
	mw.DeclarePageGetOn(reg, "/admin/articles", "/api/content/list")

	pages.GET("/articles/stale/drawer", builtin.CasbinMiddlewareForPath("/api/content/list"),
		pageview.UseRenderer(pageview.RenderBare), articlePages.ArticleStaleDrawer)
	mw.DeclarePageGetOn(reg, "/admin/articles/stale/drawer", "/api/content/list")

	pages.GET("/articles/new",
		builtin.CasbinMiddlewareForPathAs("/api/content/create", http.MethodPost),
		pageview.UseRenderer(pageview.RenderPage),
		articlePages.ArticleNewPage)
	mw.DeclarePageGetAsOn(reg, "/admin/articles/new", "/api/content/create", http.MethodPost)
	pages.GET("/articles/edit",
		mw.PageAuthz("/api/content/list"),
		pageview.UseRenderer(pageview.RenderPage),
		articlePages.ArticleEditPage)
	mw.DeclarePageGetOn(reg, "/admin/articles/edit", "/api/content/list")
	pages.POST("/articles/create", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/content/create"), articlePages.ArticleCreate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/articles/create", "/api/content/create", http.MethodPost)
	}

	pages.POST("/articles/update", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/content/update"), articlePages.ArticleUpdate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/articles/update", "/api/content/update", http.MethodPost)
	}

	pages.POST("/articles/delete", builtin.CasbinMiddlewareForPath("/api/content/delete"), articlePages.ArticleDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/articles/delete", "/api/content/delete", http.MethodPost)
	}

	pages.POST("/articles/bulk-delete", builtin.CasbinMiddlewareForPath("/api/content/delete"), articlePages.ArticlesBulkDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/articles/bulk-delete", "/api/content/delete", http.MethodPost)
	}

	articleTranslations := NewArticleTranslationHandle(contents)

	if writer, werr := i18n.NewContentWriterDefault(); werr == nil {
		articleTranslations.SetContentWriter(writer)
	}
	pages.GET("/articles/translations",
		mw.PageAuthz("/api/content/list"),
		pageview.UseRenderer(pageview.RenderPage),
		articleTranslations.ArticleTranslations)
	mw.DeclarePageGetOn(reg, "/admin/articles/translations", "/api/content/list")

	pages.POST("/articles/translations/save",
		builtin.CasbinMiddlewareForPath("/api/content/update"),
		pageview.UseRenderer(pageview.RenderPage),
		articleTranslations.SaveArticleTranslations)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/articles/translations/save", "/api/content/update", http.MethodPost)
	}

	pages.POST("/articles/score",
		builtin.CasbinMiddlewareForPath("/api/content/list"),
		pageview.UseRenderer(pageview.RenderBare),
		articlePages.ArticleScorePanel)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/articles/score", "/api/content/list", http.MethodGet)
	}

	pages.GET("/articles/seo/drawer", builtin.CasbinMiddlewareForPath("/api/content/list"),
		pageview.UseRenderer(pageview.RenderBare), articlePages.ArticleSeoDrawer)
	mw.DeclarePageGetOn(reg, "/admin/articles/seo/drawer", "/api/content/list")

	pages.GET("/articles/preview-frame", builtin.CasbinMiddlewareForPath("/api/content/list"),
		pageview.UseRenderer(pageview.RenderBare), articlePages.ArticlePreviewFrame)
	mw.DeclarePageGetOn(reg, "/admin/articles/preview-frame", "/api/content/list")

	pages.POST("/articles/link-suggestions",
		builtin.CasbinMiddlewareForPath("/api/content/list"),
		articlePages.ArticleLinkSuggestions)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/articles/link-suggestions", "/api/content/list", http.MethodGet)
	}

	pages.POST("/articles/import-preview",
		builtin.CasbinMiddlewareForPath("/api/content/list"),
		pageview.UseRenderer(pageview.RenderBare),
		articlePages.ArticleImportPreview)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/articles/import-preview", "/api/content/list", http.MethodGet)
	}
	pages.POST("/articles/import-page", pageview.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/page/create"), articlePages.ArticleImportCreate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/articles/import-page", "/api/page/create", http.MethodPost)
	}

	pages.POST("/articles/publish", builtin.CasbinMiddlewareForPath("/api/presentation/create"), articlePages.ArticlePublish)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/articles/publish", "/api/presentation/create", http.MethodPost)
	}

	pages.POST("/articles/rebuild", builtin.CasbinMiddlewareForPath("/api/presentation/rebuild"), articlePages.ArticleRebuild)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/articles/rebuild", "/api/presentation/rebuild", http.MethodPost)
	}

	pages.POST("/articles/url", builtin.CasbinMiddlewareForPath("/api/presentation/update-url"), articlePages.ArticleUpdateURL)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/articles/url", "/api/presentation/update-url", http.MethodPost)
	}

}

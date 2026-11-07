package contenthttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	mw "go_wp/pkg/middleware"
	"go_wp/internal/middleware/builtin"
	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
)

const ContentPreviewNoticeTemplate = "admin/preview_notice.html"

const ArticlesPageTemplate = "admin/content/articles.html"

const ArticleNewPageTemplate = "admin/content/article_new.html"

const ArticleTranslationsTemplate = "admin/content/article_translations.html"

const ArticleEditPageTemplate = "admin/content/article_edit.html"

const (
	SeoScoreFragmentTemplate      = "fragments/seo_score"
	ArticleImportFragmentTemplate = "fragments/article_import"

	ArticleSeoDrawerTemplate   = "admin/content/article_seo_drawer.html"
	ArticleStaleDrawerTemplate = "admin/partials/stale_pages_drawer.html"
)

func SetupContentPages(pages *gin.RouterGroup, contents contentcontract.ContentService,
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

	pages.GET("/articles/stale/drawer", builtin.CasbinMiddlewareForPath("/api/content/list"),
		pageview.UseRenderer(pageview.RenderBare), articlePages.ArticleStaleDrawer)

	pages.GET("/articles/new",
		builtin.CasbinMiddlewareForPathAs("/api/content/create", http.MethodPost),
		pageview.UseRenderer(pageview.RenderPage),
		articlePages.ArticleNewPage)
	pages.GET("/articles/edit",
		mw.PageAuthz("/api/content/list"),
		pageview.UseRenderer(pageview.RenderPage),
		articlePages.ArticleEditPage)
	pages.POST("/articles/create", builtin.CasbinMiddlewareForPath("/api/content/create"), articlePages.ArticleCreate)
	pages.POST("/articles/update", builtin.CasbinMiddlewareForPath("/api/content/update"), articlePages.ArticleUpdate)
	pages.POST("/articles/delete", builtin.CasbinMiddlewareForPath("/api/content/delete"), articlePages.ArticleDelete)

	pages.POST("/articles/bulk-delete", builtin.CasbinMiddlewareForPath("/api/content/delete"), articlePages.ArticlesBulkDelete)

	articleTranslations := NewArticleTranslationHandle(contents)

	if writer, werr := i18n.NewContentWriterDefault(); werr == nil {
		articleTranslations.SetContentWriter(writer)
	}
	pages.GET("/articles/translations",
		mw.PageAuthz("/api/content/list"),
		pageview.UseRenderer(pageview.RenderPage),
		articleTranslations.ArticleTranslations)

	pages.POST("/articles/translations/save",
		builtin.CasbinMiddlewareForPath("/api/content/update"),
		pageview.UseRenderer(pageview.RenderPage),
		articleTranslations.SaveArticleTranslations)

	pages.POST("/articles/score",
		pageview.UseRenderer(pageview.RenderBare),
		articlePages.ArticleScorePanel)

	pages.GET("/articles/seo/drawer", builtin.CasbinMiddlewareForPath("/api/content/list"),
		pageview.UseRenderer(pageview.RenderBare), articlePages.ArticleSeoDrawer)

	pages.GET("/articles/preview-frame", pageview.UseRenderer(pageview.RenderBare), articlePages.ArticlePreviewFrame)

	pages.POST("/articles/link-suggestions", articlePages.ArticleLinkSuggestions)

	pages.POST("/articles/import-preview",
		pageview.UseRenderer(pageview.RenderBare),
		articlePages.ArticleImportPreview)
	pages.POST("/articles/import-page", builtin.CasbinMiddlewareForPath("/api/page/create"), articlePages.ArticleImportCreate)
	pages.POST("/articles/publish", builtin.CasbinMiddlewareForPath("/api/presentation/create"), articlePages.ArticlePublish)
	pages.POST("/articles/rebuild", builtin.CasbinMiddlewareForPath("/api/presentation/rebuild"), articlePages.ArticleRebuild)
	pages.POST("/articles/url", builtin.CasbinMiddlewareForPath("/api/presentation/update-url"), articlePages.ArticleUpdateURL)
}

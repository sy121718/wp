package contenttemplatehttp

import (
	"github.com/gin-gonic/gin"
	pageview "go_wp/pkg/view"

	mw "go_wp/pkg/middleware"
	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

const ContentTemplatesPageTemplate = "admin/contenttemplate/content_templates.html"

func SetupContentTemplatePages(pages *gin.RouterGroup,
	templates contenttemplatecontract.ContentTemplateService,
	projects projectcontract.ProjectService, products productcontract.ProductService,
	contents contentcontract.ContentService) {
	if pages == nil {
		return
	}
	contentTemplatePages := newContentTemplatePageHandle(templates, projects, products, contents)

	pages.GET("/content-templates",
		mw.PageAuthz("/api/contenttemplate/list"),
		pageview.Slot(ContentTemplatesPageTemplate, pageview.RenderPage),
		contentTemplatePages.ContentTemplatesPage)
	pages.GET("/content-templates/edit", mw.PageAuthz("/api/contenttemplate/list"), contentTemplatePages.ContentTemplateEditPage)
}

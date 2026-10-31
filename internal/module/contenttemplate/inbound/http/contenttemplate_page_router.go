package contenttemplatehttp

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
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
		adminui.PageAuthz("/api/contenttemplate/list"),
		adminui.PageSlot(ContentTemplatesPageTemplate),
		contentTemplatePages.ContentTemplatesPage)
	pages.GET("/content-templates/edit", adminui.PageAuthz("/api/contenttemplate/list"), contentTemplatePages.ContentTemplateEditPage)
}

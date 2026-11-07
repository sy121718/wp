package contenttemplatehttp

import (
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/builder/core"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"

	"gorm.io/gorm"
)

func SetupContentTemplateRoutes(rg *mw.RouteGroup, db *gorm.DB, project projectcontract.ProjectService,
	registry core.EntitySourceRegistry) contenttemplatecontract.ContentTemplateService {
	svc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), project, registry)
	handle := NewHandle(svc)

	g := rg.Group("/contenttemplate")
	g.POST("/create", contenttemplateenums.ContenttemplateCreate, handle.Create)
	g.POST("/update", contenttemplateenums.ContenttemplateUpdate, handle.Update)

	g.POST("/activate", contenttemplateenums.ContenttemplateActivate, pageview.HTMLFragment, handle.Activate)
	g.POST("/delete", contenttemplateenums.ContenttemplateDelete, pageview.HTMLFragment, pageview.UseRenderer(pageview.RenderPage), handle.Delete)
	g.GET("/get", contenttemplateenums.ContenttemplateGet, handle.Get)
	g.GET("/list", contenttemplateenums.ContenttemplateList, handle.List)
	return svc
}

package contenttemplatehttp

import (
	"go_wp/internal/adminui"
	"go_wp/pkg/builder/core"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
	pageview "go_wp/pkg/view"

	"gorm.io/gorm"
)

func SetupContentTemplateRoutes(rg *permission.RouteGroup, db *gorm.DB, project projectcontract.ProjectService,
	registry core.EntitySourceRegistry) contenttemplatecontract.ContentTemplateService {
	svc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), project, registry)
	handle := NewHandle(svc)

	g := rg.Group("/contenttemplate")
	g.POST("/create", permission.ContenttemplateCreate, handle.Create)
	g.POST("/update", permission.ContenttemplateUpdate, handle.Update)

	g.POST("/activate", permission.ContenttemplateActivate, pageview.HTMLFragment, handle.Activate)
	g.POST("/delete", permission.ContenttemplateDelete, pageview.HTMLFragment, pageview.UseRenderer(adminui.RenderPage), handle.Delete)
	g.GET("/get", permission.ContenttemplateGet, handle.Get)
	g.GET("/list", permission.ContenttemplateList, handle.List)
	return svc
}

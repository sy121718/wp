package contenttemplatehttp

// contenttemplate_router.go — contenttemplate 模块路由自装配（0-A2）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。

import (
	"go_wp/internal/builder/core"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

// SetupContentTemplateRoutes 装配 contenttemplate 模块路由，返回模块契约。
// project 用于解析模板所属工程（content_templates.project_id 为 NOT NULL 外键）。
func SetupContentTemplateRoutes(rg *permission.RouteGroup, db *gorm.DB, project projectcontract.ProjectService,
	registry core.EntitySourceRegistry) contenttemplatecontract.ContentTemplateService {
	svc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), project, registry)
	handle := NewHandle(svc)

	g := rg.Group("/contenttemplate")
	g.POST("/create", permission.ContenttemplateCreate, handle.Create)
	g.POST("/update", permission.ContenttemplateUpdate, handle.Update)
	g.GET("/get", permission.ContenttemplateGet, handle.Get)
	g.GET("/list", permission.ContenttemplateList, handle.List)
	return svc
}

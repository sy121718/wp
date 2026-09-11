package presentationhttp

// presentation_router.go — presentation 模块路由自装配（0-A2）。

import (
	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	projectcontract "go_wp/internal/module/project/contract"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupPresentationRoutes 装配 presentation 模块路由，返回模块契约。
// project 用于解析实例所属工程（presentation_instances.project_id 为 NOT NULL 外键）；
// blocks 用于内容模板内部的全局块引用展开（core.globalref，构建期内联）；
// collections 为集合源解析器（issue #9）：模板内的集合类组件构建期展开为静态列表。
func SetupPresentationRoutes(rg *gin.RouterGroup, db *gorm.DB,
	templates contenttemplatecontract.ContentTemplateService,
	registry core.EntitySourceRegistry,
	project projectcontract.ProjectService,
	blocks blockcontract.BlockService,
	collections core.CollectionResolver) presentationcontract.PresentationService {
	svc := presentationservice.NewService(presentationmodel.NewModel(db), templates, registry, project, blocks)
	svc.SetCollectionResolver(collections)
	handle := NewHandle(svc)

	g := rg.Group("/presentation")
	g.POST("/create", handle.CreateInstance)
	g.POST("/rebuild", handle.Rebuild)
	g.GET("/get", handle.Get)
	g.GET("/list", handle.List)
	g.POST("/delete", handle.Delete)
	return svc
}

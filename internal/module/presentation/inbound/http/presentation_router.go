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
	// 按内容实体查实例（issue #14）：后台「详情页模板」页读当前绑定的模板与发布状态。
	g.GET("/get-by-entity", handle.GetByEntity)
	// 发布前预览（issue #14 验收 3）：按指定/默认模板只读渲染，不落库不激活。
	// 与 create/rebuild 分开，便于按「只读预览」单独授权。
	g.POST("/preview", handle.Preview)
	return svc
}

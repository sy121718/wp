package presentationhttp

import (
	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

func SetupPresentationRoutes(rg *permission.RouteGroup, db *gorm.DB,
	templates contenttemplatecontract.ContentTemplateService,
	registry core.EntitySourceRegistry,
	project projectcontract.ProjectService,
	blocks blockcontract.BlockService,
	collections core.CollectionResolver,
	publication pubcontract.PublicationService) presentationcontract.PresentationService {
	svc := presentationservice.NewService(presentationmodel.NewModel(db), templates, registry, project, blocks, publication)
	svc.SetCollectionResolver(collections)
	handle := NewHandle(svc)

	g := rg.Group("/presentation")
	g.POST("/create", permission.PresentationCreate, handle.CreateInstance)
	g.POST("/rebuild", permission.PresentationRebuild, handle.Rebuild)
	g.GET("/get", permission.PresentationGet, handle.Get)
	g.GET("/list", permission.PresentationList, handle.List)
	g.POST("/delete", permission.PresentationDelete, handle.Delete)

	g.GET("/get-by-entity", permission.PresentationGetByEntity, handle.GetByEntity)

	g.POST("/preview", permission.PresentationPreview, handle.Preview)

	g.POST("/update-url", permission.PresentationUpdateURL, handle.UpdateURL)

	presentationservice.StartPendingReceiptConvergenceScheduler(svc)
	return svc
}

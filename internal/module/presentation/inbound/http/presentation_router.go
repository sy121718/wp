package presentationhttp

import (
	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/pkg/builder/core"
	mw "go_wp/pkg/middleware"

	"gorm.io/gorm"
)

func SetupPresentationRoutes(rg *mw.RouteGroup, db *gorm.DB,
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
	g.POST("/create", presentationenums.PresentationCreate, handle.CreateInstance)
	g.POST("/rebuild", presentationenums.PresentationRebuild, handle.Rebuild)
	g.GET("/get", presentationenums.PresentationGet, handle.Get)
	g.GET("/list", presentationenums.PresentationList, handle.List)
	g.POST("/delete", presentationenums.PresentationDelete, handle.Delete)

	g.GET("/get-by-entity", presentationenums.PresentationGetByEntity, handle.GetByEntity)

	g.POST("/preview", presentationenums.PresentationPreview, handle.Preview)

	g.POST("/update-url", presentationenums.PresentationUpdateURL, handle.UpdateURL)

	presentationservice.StartPendingReceiptConvergenceScheduler(svc)
	return svc
}

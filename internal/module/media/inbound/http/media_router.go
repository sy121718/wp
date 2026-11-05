package mediahttp

import (
	"go_wp/internal/middleware/builtin"
	mediacontract "go_wp/internal/module/media/contract"
	mediaenums "go_wp/internal/module/media/enums"
	mediamodel "go_wp/internal/module/media/model"
	mediaservice "go_wp/internal/module/media/service"
	mw "go_wp/pkg/middleware"

	"gorm.io/gorm"
)

func SetupMediaRoutes(rg *mw.RouteGroup, db *gorm.DB) mediacontract.MediaService {
	am := mediamodel.NewAttachmentModel(db)
	cm := mediamodel.NewFileCategoryModel(db)
	vm := mediamodel.NewMediaVariantModel(db)
	svc := mediaservice.NewService(am, cm, vm)
	handle := NewHandle(svc)

	mediaservice.RegisterVariantTaskHandler(db)

	mediaservice.StartMediaReconcileScheduler(svc)

	g := rg.Group("/media", builtin.SessionAuthMiddleware())
	{
		g.POST("/upload", mediaenums.MediaUpload, handle.Upload)
		g.GET("/list", mediaenums.MediaList, handle.List)
		g.GET("/detail", mediaenums.MediaDetail, handle.Detail)
		g.POST("/delete", mediaenums.MediaDelete, handle.Delete)
		g.POST("/update", mediaenums.MediaUpdate, handle.UpdateAttachment)

		g.POST("/replace", mediaenums.MediaReplace, handle.Replace)
		g.GET("/references", mediaenums.MediaReferences, handle.References)
		g.GET("/category/tree", mediaenums.MediaCategoryTree, handle.CategoryTree)
		g.POST("/category/create", mediaenums.MediaCategoryCreate, handle.CategoryCreate)
		g.POST("/category/update", mediaenums.MediaCategoryUpdate, handle.CategoryUpdate)
		g.POST("/category/delete", mediaenums.MediaCategoryDelete, handle.CategoryDelete)

		g.GET("/download", mediaenums.MediaDownload, handle.Download)
		g.GET("/download/batch", mediaenums.MediaDownloadBatch, handle.DownloadBatch)
		g.POST("/variants/generate", mediaenums.MediaVariantsGenerate, handle.GenerateVariants)
	}
	return svc
}

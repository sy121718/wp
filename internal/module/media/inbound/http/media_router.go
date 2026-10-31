package mediahttp

import (
	"go_wp/internal/middleware/builtin"
	mediacontract "go_wp/internal/module/media/contract"
	mediamodel "go_wp/internal/module/media/model"
	mediaservice "go_wp/internal/module/media/service"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

func SetupMediaRoutes(rg *permission.RouteGroup, db *gorm.DB) mediacontract.MediaService {
	am := mediamodel.NewAttachmentModel(db)
	cm := mediamodel.NewFileCategoryModel(db)
	vm := mediamodel.NewMediaVariantModel(db)
	svc := mediaservice.NewService(am, cm, vm)
	handle := NewHandle(svc)

	mediaservice.RegisterVariantTaskHandler(db)

	mediaservice.StartMediaReconcileScheduler(svc)

	g := rg.Group("/media", builtin.SessionAuthMiddleware())
	{
		g.POST("/upload", permission.MediaUpload, handle.Upload)
		g.GET("/list", permission.MediaList, handle.List)
		g.GET("/detail", permission.MediaDetail, handle.Detail)
		g.POST("/delete", permission.MediaDelete, handle.Delete)
		g.POST("/update", permission.MediaUpdate, handle.UpdateAttachment)

		g.POST("/replace", permission.MediaReplace, handle.Replace)
		g.GET("/references", permission.MediaReferences, handle.References)
		g.GET("/category/tree", permission.MediaCategoryTree, handle.CategoryTree)
		g.POST("/category/create", permission.MediaCategoryCreate, handle.CategoryCreate)
		g.POST("/category/update", permission.MediaCategoryUpdate, handle.CategoryUpdate)
		g.POST("/category/delete", permission.MediaCategoryDelete, handle.CategoryDelete)

		g.GET("/download", permission.MediaDownload, handle.Download)
		g.GET("/download/batch", permission.MediaDownloadBatch, handle.DownloadBatch)
		g.POST("/variants/generate", permission.MediaVariantsGenerate, handle.GenerateVariants)
	}
	return svc
}

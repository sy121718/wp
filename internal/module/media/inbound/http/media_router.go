package mediahttp

import (
	"go_wp/internal/middleware/builtin"
	mediacontract "go_wp/internal/module/media/contract"
	mediamodel "go_wp/internal/module/media/model"
	mediaservice "go_wp/internal/module/media/service"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

// SetupMediaRoutes 注册媒体模块路由，返回契约接口供其他模块引用。
// 同时把图片变体生成的 asynq handler 注册进现有队列 worker（见 service.RegisterVariantTaskHandler）。
func SetupMediaRoutes(rg *permission.RouteGroup, db *gorm.DB) mediacontract.MediaService {
	am := mediamodel.NewAttachmentModel(db)
	cm := mediamodel.NewFileCategoryModel(db)
	vm := mediamodel.NewMediaVariantModel(db)
	svc := mediaservice.NewService(am, cm, vm)
	handle := NewHandle(svc)

	// 变体生成任务：handler 注册（queue.Init 前暂存、Init 后即时挂 mux，无时序要求）。
	mediaservice.RegisterVariantTaskHandler(db)

	// 存储巡检调度：只读对账（文件系统 ↔ 数据库）+ 变体补偿重放。
	// 这两个入口此前没有任何调用方，而它们盯的两种不一致都只能靠时间驱动发现：
	// 对账找不到「有记录没文件 / 有文件没记录 / 草稿残留」的第二个驱动源，
	// 变体重试耗尽后也没有任何人会再碰那条附件（详见 service/media_reconcile_scheduler.go）。
	// 装配在这里启动一次：本函数每个进程只会被调用一次，调度器也就只起一个 goroutine。
	mediaservice.StartMediaReconcileScheduler(svc)

	g := rg.Group("/media", builtin.SessionAuthMiddleware())
	{
		g.POST("/upload", permission.MediaUpload, handle.Upload)
		g.GET("/list", permission.MediaList, handle.List)
		g.GET("/detail", permission.MediaDetail, handle.Detail)
		g.POST("/delete", permission.MediaDelete, handle.Delete)
		g.POST("/update", permission.MediaUpdate, handle.UpdateAttachment)
		// 媒体中心（02-B，迁移 067）：换图（URL 不变 + generation+1）与引用查询。
		// refs 写入侧不暴露 HTTP：只由构建期经 contract.SyncReferencesFromHTML 调用。
		g.POST("/replace", permission.MediaReplace, handle.Replace)
		g.GET("/references", permission.MediaReferences, handle.References)
		g.GET("/category/tree", permission.MediaCategoryTree, handle.CategoryTree)
		g.POST("/category/create", permission.MediaCategoryCreate, handle.CategoryCreate)
		g.POST("/category/update", permission.MediaCategoryUpdate, handle.CategoryUpdate)
		g.POST("/category/delete", permission.MediaCategoryDelete, handle.CategoryDelete)
		// 图片变体与打包下载（048 改造；权限点 seed 见 048_media_variant.sql）。
		g.GET("/download", permission.MediaDownload, handle.Download)
		g.GET("/download/batch", permission.MediaDownloadBatch, handle.DownloadBatch)
		g.POST("/variants/generate", permission.MediaVariantsGenerate, handle.GenerateVariants)
	}
	return svc
}

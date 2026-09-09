package mediahttp

import (
	"go_wp/internal/middleware/builtin"
	mediacontract "go_wp/internal/module/media/contract"
	mediamodel "go_wp/internal/module/media/model"
	mediaservice "go_wp/internal/module/media/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupMediaRoutes 注册媒体模块路由，返回契约接口供其他模块引用。
// 同时把图片变体生成的 asynq handler 注册进现有队列 worker（见 service.RegisterVariantTaskHandler）。
func SetupMediaRoutes(rg *gin.RouterGroup, db *gorm.DB) mediacontract.MediaService {
	am := mediamodel.NewAttachmentModel(db)
	cm := mediamodel.NewFileCategoryModel(db)
	vm := mediamodel.NewMediaVariantModel(db)
	svc := mediaservice.NewService(am, cm, vm)
	handle := NewHandle(svc)

	// 变体生成任务：handler 注册（queue.Init 前暂存、Init 后即时挂 mux，无时序要求）。
	mediaservice.RegisterVariantTaskHandler(db)

	g := rg.Group("/media", builtin.SessionAuthMiddleware())
	{
		g.POST("/upload", handle.Upload)
		g.GET("/list", handle.List)
		g.GET("/detail", handle.Detail)
		g.POST("/delete", handle.Delete)
		g.POST("/update", handle.UpdateAttachment)
		// 媒体中心（02-B，迁移 067）：换图（URL 不变 + generation+1）与引用查询。
		// refs 写入侧不暴露 HTTP：只由构建期经 contract.SyncReferencesFromHTML 调用。
		g.POST("/replace", handle.Replace)
		g.GET("/references", handle.References)
		g.GET("/category/tree", handle.CategoryTree)
		g.POST("/category/create", handle.CategoryCreate)
		g.POST("/category/update", handle.CategoryUpdate)
		g.POST("/category/delete", handle.CategoryDelete)
		// 图片变体与打包下载（048 改造；权限点 seed 见 048_media_variant.sql）。
		g.GET("/download", handle.Download)
		g.GET("/download/batch", handle.DownloadBatch)
		g.POST("/variants/generate", handle.GenerateVariants)
	}
	return svc
}

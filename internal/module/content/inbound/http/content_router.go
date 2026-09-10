package contenthttp

// content_router.go — content 模块路由自装配（0-A2）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。

import (
	contentcontract "go_wp/internal/module/content/contract"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupContentRoutes 装配 content 模块路由，返回模块契约。
func SetupContentRoutes(rg *gin.RouterGroup, db *gorm.DB) contentcontract.ContentService {
	svc := contentservice.NewService(contentmodel.NewModel(db))
	handle := NewHandle(svc)

	g := rg.Group("/content")
	g.POST("/create", handle.Create)
	g.POST("/update", handle.Update)
	g.GET("/get", handle.Get)
	g.GET("/list", handle.List)
	// 集合源元数据：内置组件集合字段白名单 + 工作台字段下拉。
	g.GET("/collections", handle.Collections)
	g.POST("/delete", handle.Delete)
	return svc
}

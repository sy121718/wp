package contenthttp

// content_router.go — content 模块路由自装配（0-A2）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。

import (
	"go_wp/internal/builder/core"
	contentcontract "go_wp/internal/module/content/contract"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupContentRoutes 装配 content 模块路由，返回模块契约。
//
// collections 为集合源元数据聚合端口（装配期的集合源注册表）：集合源已跨模块
// （商品同样是集合源，issue #9），元数据接口必须返回全量 —— 注册表在装配期
// 后续步骤才填充完成，这里只持有指针，请求到来时已是全量。
func SetupContentRoutes(rg *gin.RouterGroup, db *gorm.DB, collections core.CollectionSchemaProvider) contentcontract.ContentService {
	svc := contentservice.NewService(contentmodel.NewModel(db))
	handle := NewHandle(svc)
	handle.SetCollectionSchemas(collections)

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

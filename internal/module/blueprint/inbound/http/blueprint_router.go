package blueprinthttp

// blueprint_router.go — blueprint 模块路由自装配（0-B）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。

import (
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintmodel "go_wp/internal/module/blueprint/model"
	blueprintservice "go_wp/internal/module/blueprint/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupBlueprintRoutes 装配 blueprint 模块路由，返回模块契约。
func SetupBlueprintRoutes(rg *gin.RouterGroup, db *gorm.DB) blueprintcontract.BlueprintService {
	svc := blueprintservice.NewService(blueprintmodel.NewModel(db))
	handle := NewHandle(svc)

	g := rg.Group("/blueprint")
	g.POST("/create", handle.Create)
	g.POST("/update", handle.Update)
	g.POST("/publish", handle.Publish)
	g.GET("/get", handle.Get)
	g.GET("/list", handle.List)
	g.POST("/delete", handle.Delete)
	g.GET("/init", handle.Init)
	return svc
}

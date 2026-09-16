package blueprinthttp

// blueprint_router.go — blueprint 模块路由自装配（0-B）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。

import (
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintmodel "go_wp/internal/module/blueprint/model"
	blueprintservice "go_wp/internal/module/blueprint/service"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

// SetupBlueprintRoutes 装配 blueprint 模块路由，返回模块契约。
func SetupBlueprintRoutes(rg *permission.RouteGroup, db *gorm.DB) blueprintcontract.BlueprintService {
	svc := blueprintservice.NewService(blueprintmodel.NewModel(db))
	handle := NewHandle(svc)

	g := rg.Group("/blueprint")
	g.POST("/create", permission.BlueprintCreate, handle.Create)
	g.POST("/update", permission.BlueprintUpdate, handle.Update)
	g.POST("/publish", permission.BlueprintPublish, handle.Publish)
	g.GET("/get", permission.BlueprintGet, handle.Get)
	g.GET("/list", permission.BlueprintList, handle.List)
	g.POST("/delete", permission.BlueprintDelete, handle.Delete)
	g.GET("/init", permission.BlueprintInit, handle.Init)
	return svc
}

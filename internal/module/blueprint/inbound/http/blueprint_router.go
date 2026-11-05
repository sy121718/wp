package blueprinthttp

import (
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintenums "go_wp/internal/module/blueprint/enums"
	blueprintmodel "go_wp/internal/module/blueprint/model"
	blueprintservice "go_wp/internal/module/blueprint/service"
	mw "go_wp/pkg/middleware"

	"gorm.io/gorm"
)

func SetupBlueprintRoutes(rg *mw.RouteGroup, db *gorm.DB) blueprintcontract.BlueprintService {
	svc := blueprintservice.NewService(blueprintmodel.NewModel(db))
	handle := NewHandle(svc)

	g := rg.Group("/blueprint")
	g.POST("/create", blueprintenums.BlueprintCreate, handle.Create)
	g.POST("/update", blueprintenums.BlueprintUpdate, handle.Update)
	g.POST("/publish", blueprintenums.BlueprintPublish, handle.Publish)
	g.GET("/get", blueprintenums.BlueprintGet, handle.Get)
	g.GET("/list", blueprintenums.BlueprintList, handle.List)
	g.POST("/delete", blueprintenums.BlueprintDelete, handle.Delete)
	g.GET("/init", blueprintenums.BlueprintInit, handle.Init)
	return svc
}

package blockhttp

import (
	blockcontract "go_wp/internal/module/block/contract"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"

	"go_wp/internal/middleware/builtin"

	"gorm.io/gorm"
)

func SetupBlockRoutes(rg *permission.RouteGroup, db *gorm.DB, projects projectcontract.ProjectService) blockcontract.BlockService {
	svc := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	h := &Handle{svc: svc}

	g := rg.Group("/block", builtin.SessionAuthMiddleware())
	g.GET("/list", permission.BlockList, h.List)
	g.GET("/detail", permission.BlockDetail, h.Detail)
	g.POST("/create", permission.BlockCreate, h.Create)
	g.POST("/update", permission.BlockUpdate, h.Update)
	g.POST("/delete", permission.BlockDelete, h.Delete)

	g.POST("/bulk-delete", permission.BlockBulkDelete, h.BulkDelete)
	g.POST("/clone", permission.BlockClone, h.CloneAST)
	return svc
}

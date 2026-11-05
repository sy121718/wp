package blockhttp

import (
	blockcontract "go_wp/internal/module/block/contract"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/middleware/builtin"
	blockenums "go_wp/internal/module/block/enums"
	mw "go_wp/pkg/middleware"

	"gorm.io/gorm"
)

func SetupBlockRoutes(rg *mw.RouteGroup, db *gorm.DB, projects projectcontract.ProjectService) blockcontract.BlockService {
	svc := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	h := &Handle{svc: svc}

	g := rg.Group("/block", builtin.SessionAuthMiddleware())
	g.GET("/list", blockenums.BlockList, h.List)
	g.GET("/detail", blockenums.BlockDetail, h.Detail)
	g.POST("/create", blockenums.BlockCreate, h.Create)
	g.POST("/update", blockenums.BlockUpdate, h.Update)
	g.POST("/delete", blockenums.BlockDelete, h.Delete)

	g.POST("/bulk-delete", blockenums.BlockBulkDelete, h.BulkDelete)
	g.POST("/clone", blockenums.BlockClone, h.CloneAST)
	return svc
}

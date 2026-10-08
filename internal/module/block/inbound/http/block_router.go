package blockhttp

// block_router.go — 全局块 REST API 的注册落点（JSON 模式）。
//
// 只做注册：装配 model / service / Handle 并注册路由，handler 与错误映射在 block_handle.go。
// 路由注册只出现在 *_router.go，门禁 scripts/check-route-registration-placement.sh 守这条。

import (
	blockcontract "go_wp/internal/module/block/contract"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"

	"go_wp/internal/middleware/builtin"

	"gorm.io/gorm"
)

// SetupBlockRoutes 注册全局块路由，返回块契约（供 page 构建装配与 dashboard 使用）。
func SetupBlockRoutes(rg *permission.RouteGroup, db *gorm.DB, projects projectcontract.ProjectService) blockcontract.BlockService {
	svc := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	h := &Handle{svc: svc}

	g := rg.Group("/block", builtin.SessionAuthMiddleware())
	g.GET("/list", permission.BlockList, h.List)
	g.GET("/detail", permission.BlockDetail, h.Detail)
	g.POST("/create", permission.BlockCreate, h.Create)
	g.POST("/update", permission.BlockUpdate, h.Update)
	g.POST("/delete", permission.BlockDelete, h.Delete)
	g.POST("/clone", permission.BlockClone, h.CloneAST)
	return svc
}

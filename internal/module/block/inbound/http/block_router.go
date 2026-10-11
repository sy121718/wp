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
	// 批量删除：**独立权限点** block:bulk_delete。
	//
	// 曾与单条删除共用一个 code，但那在本仓的权限模型里是不成立的：sys_permission 每个 code
	// 只有一列 api_path/api_method，permission.SyncToDB 按声明对齐这两列 —— 两条 API 路径共用
	// 一个 code 时，同步只能留下其中一条，另一条上的策略随即失配（超管 403、角色静默失权）。
	// 权限点的*能力*仍然是「删区块」这一档，升级期的授权延续由迁移 594 复制既有
	// block:delete 主体（且只复制这些主体）兜住。
	// 上限与去重由 pkg/bulkids.Normalize 在服务端强制 —— 前端也有一次同口径的拦截，但那只是快速反馈。
	g.POST("/bulk-delete", permission.BlockBulkDelete, h.BulkDelete)
	g.POST("/clone", permission.BlockClone, h.CloneAST)
	return svc
}

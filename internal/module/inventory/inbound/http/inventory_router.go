// inventory_router.go — inventory 模块路由自装配（issue #15 / #16）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。
package inventoryhttp

import (
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	projectcontract "go_wp/internal/module/project/contract"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupInventoryRoutes 装配 inventory 模块路由，返回模块契约。
// project 用于把「未指定工程」解析为唯一工程（warehouses.project_id 为 NOT NULL 外键）。
//
// 返回的契约同时实现了 product 契约定义的 VariantStockPort（ResolveWarehouse /
// EnsureVariantStock）—— 顶层装配时注入商品模块（依赖方向 inventory → product）。
func SetupInventoryRoutes(rg *gin.RouterGroup, db *gorm.DB, project projectcontract.ProjectService) inventorycontract.InventoryService {
	svc := inventoryservice.NewService(inventorymodel.NewModel(db), project)
	handle := NewHandle(svc)

	g := rg.Group("/inventory")
	// 仓库（验收 1）：工程内第一个仓自动成为默认仓，显式 isDefault 即切换默认仓。
	g.GET("/warehouse/list", handle.ListWarehouses)
	g.GET("/warehouse/get", handle.GetWarehouse)
	g.POST("/warehouse/create", handle.CreateWarehouse)
	g.POST("/warehouse/update", handle.UpdateWarehouse)
	g.POST("/warehouse/delete", handle.DeleteWarehouse)
	// 库存记录（验收 2/3/4）：ensure 是幂等的「确保某 SKU 在某仓有一行」，
	// list 支持按 SKU 查各仓库存（维度是 SKU × 仓库）。
	g.GET("/stock/list", handle.ListStocks)
	g.GET("/stock/sku", handle.ListStocksBySKU)
	g.GET("/stock/get", handle.GetStock)
	g.POST("/stock/ensure", handle.EnsureStock)

	// 库存变动与流水（issue #16 验收 1–5）：真源行锁增减 + 流水 + 原因字典 + 物料清单。
	g.POST("/stock/change", handle.ChangeStock)
	g.POST("/stock/deduct", handle.DeductStock)
	g.GET("/movement/list", handle.ListMovements)
	g.GET("/reason/list", handle.ListReasons)
	g.POST("/reason/create", handle.CreateReason)
	g.POST("/reason/update", handle.UpdateReason)
	g.POST("/bom/set", handle.SetBOM)
	g.GET("/bom/get", handle.GetBOM)

	// 货源（issue #17）：一张表承载全部进货来源 —— 外部供应商 / 集团内关联公司 / 自家工厂
	// 用类型区分，关联方标志独立成列（报表区分），config 承载异构对接扩展信息。
	// list 的 type / relatedParty / status / keyword 都是可组合的报表筛选维度，
	// summary 按「类型 × 关联方」分组给出交叉计数。
	g.GET("/source/list", handle.ListSources)
	g.GET("/source/get", handle.GetSource)
	g.GET("/source/summary", handle.SourceSummary)
	g.POST("/source/create", handle.CreateSource)
	g.POST("/source/update", handle.UpdateSource)
	g.POST("/source/delete", handle.DeleteSource)

	// 商品侧缓存同步与对账（issue #16 验收 6/7）：提交后的独立步骤，不进变动事务。
	g.POST("/cache/sync", handle.SyncStockCache)
	g.POST("/cache/reconcile", handle.ReconcileStockCache)
	return svc
}

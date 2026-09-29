// inventory_router.go — inventory 模块路由自装配（issue #15 / #16）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。
package inventoryhttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	"go_wp/internal/middleware/builtin"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
)

// productCatalogConsumer 需要商品契约做「商品 → 变体」下拉的后台页面处理器。
type productCatalogConsumer interface {
	setProductCatalog(products productcontract.ProductService)
}

// pageCatalogConsumers 装配期登记的后台页面处理器（写一次，之后只读）。
var pageCatalogConsumers []productCatalogConsumer

// SetProductCatalog 为已注册的后台页面补注商品契约（下拉数据源）。
//
// 为什么必须后置注入：商品模块的 Setup 需要本模块的 InventoryService（VariantStockPort），
// 本模块的后台页面又需要商品契约做「商品 → 变体」下拉 —— 两边互为依赖，固定顺序装配不出来。
// 与本仓 runtimefragment.SetBundleProvider 同一手法：装配期注入一次，之后只读。
// 未注入时页面照常渲染，只是下拉为空（handler 侧对 nil 做降级，不 panic）。
func SetProductCatalog(products productcontract.ProductService) {
	for _, consumer := range pageCatalogConsumers {
		consumer.setProductCatalog(products)
	}
}

// SetupInventoryRoutes 装配 inventory 模块路由，返回模块契约。
//
// rg 是 API 组（前缀 /api，三层链），pages 是后台页面组（前缀 /admin，Session + CSRF +
// 权限上下文 / 侧栏菜单树由装配层统一挂在组上）—— pages 为 nil 时跳过页面注册，
// 与 rg 为 nil 的早退同构。
//
// project 用于把「未指定工程」解析为唯一工程（warehouses.project_id 为 NOT NULL 外键）。
//
// 返回的契约同时实现了 product 契约定义的 VariantStockPort（ResolveWarehouse /
// EnsureVariantStock）—— 顶层装配时注入商品模块（依赖方向 inventory → product）。
// 页面所需的商品契约反向依赖商品模块，因此不在参数表里，由装配收尾的 SetProductCatalog 补注。
func SetupInventoryRoutes(rg *permission.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	project projectcontract.ProjectService) inventorycontract.InventoryService {
	svc := inventoryservice.NewService(inventorymodel.NewModel(db), project)
	// 第三方仓凭据的加密密钥（config.yaml 的 app.secret）：与 mail / webhook 同一模式 ——
	// 模块自己不读配置，装配层读进来注入。读不到时不注入：凭据写入会明确报
	// ErrWarehouseCredentialKeyMissing，而不是退回明文落库（见 service 的 applyCredential）。
	if v, err := config.GetViper(); err == nil && v != nil {
		svc.SetCipherSecret(v.GetString("app.secret"))
	}
	handle := NewHandle(svc)

	g := rg.Group("/inventory")
	// 仓库（验收 1）：工程内第一个仓自动成为默认仓，显式 isDefault 即切换默认仓。
	g.GET("/warehouse/list", permission.InventoryWarehouseList, handle.ListWarehouses)
	g.GET("/warehouse/get", permission.InventoryWarehouseGet, handle.GetWarehouse)
	g.POST("/warehouse/create", permission.InventoryWarehouseCreate, handle.CreateWarehouse)
	g.POST("/warehouse/update", permission.InventoryWarehouseUpdate, handle.UpdateWarehouse)
	g.POST("/warehouse/delete", permission.InventoryWarehouseDelete, handle.DeleteWarehouse)
	// 库存记录（验收 2/3/4）：ensure 是幂等的「确保某 SKU 在某仓有一行」，
	// list 支持按 SKU 查各仓库存（维度是 SKU × 仓库）。
	g.GET("/stock/list", permission.InventoryStockList, handle.ListStocks)
	g.GET("/stock/sku", permission.InventoryStockSKU, handle.ListStocksBySKU)
	g.GET("/stock/get", permission.InventoryStockGet, handle.GetStock)
	g.POST("/stock/ensure", permission.InventoryStockEnsure, handle.EnsureStock)

	// 库存变动与流水（issue #16 验收 1–5）：真源行锁增减 + 流水 + 原因字典 + 物料清单。
	g.POST("/stock/change", permission.InventoryStockChange, handle.ChangeStock)
	g.POST("/stock/deduct", permission.InventoryStockDeduct, handle.DeductStock)
	g.GET("/movement/list", permission.InventoryMovementList, handle.ListMovements)
	g.GET("/reason/list", permission.InventoryReasonList, handle.ListReasons)
	g.POST("/reason/create", permission.InventoryReasonCreate, handle.CreateReason)
	g.POST("/reason/update", permission.InventoryReasonUpdate, handle.UpdateReason)
	g.POST("/bom/set", permission.InventoryBomSet, handle.SetBOM)
	g.GET("/bom/get", permission.InventoryBomGet, handle.GetBOM)

	// 货源（issue #17）：一张表承载全部进货来源 —— 外部供应商 / 集团内关联公司 / 自家工厂
	// 用类型区分，关联方标志独立成列（报表区分），config 承载异构对接扩展信息。
	// list 的 type / relatedParty / status / keyword 都是可组合的报表筛选维度，
	// summary 按「类型 × 关联方」分组给出交叉计数。
	g.GET("/source/list", permission.InventorySourceList, handle.ListSources)
	g.GET("/source/get", permission.InventorySourceGet, handle.GetSource)
	g.GET("/source/summary", permission.InventorySourceSummary, handle.SourceSummary)
	g.POST("/source/create", permission.InventorySourceCreate, handle.CreateSource)
	g.POST("/source/update", permission.InventorySourceUpdate, handle.UpdateSource)
	g.POST("/source/delete", permission.InventorySourceDelete, handle.DeleteSource)

	// 采购单与入库（issue #18）：采购单（来源 = #17 的货源）→ 收货入库（复用 #16 的 ChangeStock）。
	// receipt 支持按行分批累加已入库数量，带幂等键防重复入库；production 是自家工厂
	// 生产入库（无采购单，成本价手工填写）；history 是某 SKU 的进货历史。
	g.GET("/purchase/list", permission.InventoryPurchaseList, handle.ListPurchaseOrders)
	g.GET("/purchase/get", permission.InventoryPurchaseGet, handle.GetPurchaseOrder)
	g.GET("/purchase/history", permission.InventoryPurchaseHistory, handle.ListPurchaseHistory)
	g.POST("/purchase/create", permission.InventoryPurchaseCreate, handle.CreatePurchaseOrder)
	g.POST("/purchase/update", permission.InventoryPurchaseUpdate, handle.UpdatePurchaseOrder)
	g.POST("/purchase/receipt", permission.InventoryPurchaseReceipt, handle.RegisterReceipt)
	g.POST("/purchase/production", permission.InventoryPurchaseProduction, handle.RegisterProductionInbound)

	// 商品侧缓存同步与对账（issue #16 验收 6/7）：提交后的独立步骤，不进变动事务。

	// 后台页面（issue #15 / #16 / #17 / #18）：GET 渲染完整页，写动作复用上面这些 API 的
	// 权限点 —— CasbinMiddlewareForPath 的路径是权限点声明的真源，一个字符都不能改。
	if pages != nil {
		// 商品契约（下拉数据源）留待装配收尾补注，见 SetProductCatalog。
		inventoryPages := NewInventoryPageHandle(svc, project, nil)
		pageCatalogConsumers = append(pageCatalogConsumers, inventoryPages)
		pages.GET("/inventory", inventoryPages.InventoryPage)
		// 拆页（2026-09 评审第三轮）：仓库与原因字典各自独立成页 —— 原先它们与
		// 「手动改库存 / 看流水」挤在同一页靠 <details> 折叠，折叠不是解法。
		pages.GET("/inventory/warehouses", inventoryPages.InventoryWarehousesPage)
		pages.GET("/inventory/reasons", inventoryPages.InventoryReasonsPage)
		pages.POST("/inventory/warehouse/create", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/create"), inventoryPages.InventoryWarehouseCreate)
		pages.POST("/inventory/warehouse/update", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), inventoryPages.InventoryWarehouseUpdate)
		pages.POST("/inventory/warehouse/default", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), inventoryPages.InventoryWarehouseDefault)
		pages.POST("/inventory/warehouse/delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), inventoryPages.InventoryWarehouseDelete)
		// 批量删除复用单条删除的权限点：批量不是新能力，只是把 N 次单条动作压成一次提交。
		pages.POST("/inventory/warehouses/bulk-delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), inventoryPages.InventoryWarehousesBulkDelete)
		// 库存变动与原因字典（issue #16）：变动走真源行锁 + 流水，原因新建走原因字典。
		pages.POST("/inventory/stock/change", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), inventoryPages.InventoryStockChange)
		// 库存页行内编辑外部编码（迁移 251 的可编辑入口）：**复用**本页唯一写入口的权限点，
		// 不新增权限点、不新增 authorizedAPI 路由 —— 登记外码不是新能力，而是库存行属性的就地维护，
		// 与「库存调整」同属 inventory_stocks 的写权限（能改这个仓库存的人，才该能改它的外码映射）。
		// CasbinMiddlewareForPath 的参数是权限点声明的真源，一个字符都不能改。
		pages.POST("/inventory/external-sku", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), inventoryPages.InventoryExternalSKUUpdate)
		// 库存页行内编辑「跟踪开关 + 数量」（迁移 261）：同样**复用**本页唯一写入口的权限点，
		// 不新增权限点、不新增 authorizedAPI 路由 —— 切开关与改数量都落在同一条
		// inventory_stocks 写权限上（能改这个仓库存的人，才该能改它跟不跟踪）。
		// 数量本身仍走变动契约（手工调整）并写流水，行内表单只是入口。
		pages.POST("/inventory/stock/tracking", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), inventoryPages.InventoryStockTrackingUpdate)
		pages.POST("/inventory/reason/create", builtin.CasbinMiddlewareForPath("/api/inventory/reason/create"), inventoryPages.InventoryReasonCreate)
		// 启停 / 改名：内置原因改名由 service 拒绝（它的 key 由系统按 code 派生），停用照常。
		pages.POST("/inventory/reason/update", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), inventoryPages.InventoryReasonUpdate)
		pages.POST("/inventory/reasons/bulk-status", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), inventoryPages.InventoryReasonsBulkStatus)

		// 货源管理页（issue #17）：类型与关联方两个结构化维度支撑报表区分。
		sourcePages := NewInventorySourcePageHandle(svc, project)
		pages.GET("/inventory/sources", sourcePages.InventorySourcesPage)
		pages.POST("/inventory/sources/create", builtin.CasbinMiddlewareForPath("/api/inventory/source/create"), sourcePages.InventorySourceCreate)
		pages.POST("/inventory/sources/update", builtin.CasbinMiddlewareForPath("/api/inventory/source/update"), sourcePages.InventorySourceUpdate)
		pages.POST("/inventory/sources/delete", builtin.CasbinMiddlewareForPath("/api/inventory/source/delete"), sourcePages.InventorySourceDelete)
		pages.POST("/inventory/sources/bulk-delete", builtin.CasbinMiddlewareForPath("/api/inventory/source/delete"), sourcePages.InventorySourcesBulkDelete)

		// 采购入库页（issue #18）：采购单 → 逐行收货入库（复用 #16 的变动契约）+ 生产入库 + 进货历史。
		purchasePages := NewInventoryPurchasePageHandle(svc, project, nil)
		pageCatalogConsumers = append(pageCatalogConsumers, purchasePages)
		pages.GET("/inventory/purchases", purchasePages.InventoryPurchasesPage)
		pages.POST("/inventory/purchases/create", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/create"), purchasePages.InventoryPurchaseCreate)
		pages.POST("/inventory/purchases/receipt", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/receipt"), purchasePages.InventoryPurchaseReceipt)
		pages.POST("/inventory/purchases/production", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/production"), purchasePages.InventoryPurchaseProduction)
		// 生产入库（自家工厂）属于「有单据来源的手工入库」，表单留在采购入库页：
		// 它的页面 action 是 /admin/inventory/purchases/production，权限点仍是
		// api/inventory/purchase/production（权限点是声明真源，不随页面走）。
		// 库存管理页只保留「库存调整（盘点 / 报损）」这一个写入口，因此这里不再暴露
		// /admin/inventory/production —— 留一个没有表单指向的页面路由只会变成第二个入口。
	}

	return svc
}

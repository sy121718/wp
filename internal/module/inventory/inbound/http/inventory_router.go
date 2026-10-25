// inventory_router.go — inventory 模块的 **API** 装配（issue #15 / #16）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）；
// 后台页面的注册在 inventory_page_router.go。
package inventoryhttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
)

// 页面下拉只需要商品的**只读**变体清单口（VariantOptionsReader）：
// 两页都不该拿到商品的写能力，也不该为了「取一个变体」把详情整份读出来。
// productCatalogConsumer 需要商品契约做「商品 → 变体」下拉的后台页面处理器。
type productCatalogConsumer interface {
	setProductCatalog(products productcontract.VariantOptionsReader)
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
	// 装配期类型断言而不是把 catalogProduct 写进参数：SetupProductRoutes 的返回值是
	// ProductService 接口，实现方（product 的 Service）同时实现变体清单口。
	// 断言失败是**两处接口不同步**（改了名字 / 换了实现），属于启动期该暴露的缺陷 ——
	// 静默降级成「下拉空」会让运营以为「这个工程没有商品」。
	catalog, ok := products.(productcontract.VariantOptionsReader)
	if !ok {
		panic("inventory.SetProductCatalog: 商品契约未实现 VariantOptionsReader（两处接口不同步）")
	}
	for _, consumer := range pageCatalogConsumers {
		consumer.setProductCatalog(catalog)
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

	// 库存预警（迁移 624 / 623，W4）：只统计跟踪数量的行（无限库存永远「有货」）。
	g.GET("/stock/alert", permission.InventoryStockAlert, handle.StockAlert)

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

	// 调拨单（迁移 630，W3）：仓库间移库，出货与签收**各需凭证附件**。
	// 六条独立权限点（出货与签收是两个不同的人做的事，见 permission/codes.go）。
	transferPages := NewInventoryTransferPageHandle(svc, project, nil)
	tg := rg.Group("/inventory/transfer")
	tg.GET("/list", permission.InventoryTransferList, transferPages.InventoryTransferListJSON)
	tg.GET("/get", permission.InventoryTransferGet, transferPages.InventoryTransferDetail)
	tg.POST("/create", permission.InventoryTransferCreate, transferPages.InventoryTransferCreate)
	tg.POST("/ship", permission.InventoryTransferShip, transferPages.InventoryTransferShip)
	tg.POST("/receive", permission.InventoryTransferReceive, transferPages.InventoryTransferReceive)
	tg.POST("/cancel", permission.InventoryTransferCancel, transferPages.InventoryTransferCancel)

	// 商品侧缓存同步与对账（issue #16 验收 6/7）：提交后的独立步骤，不进变动事务。

	// 后台页面：注册在 inventory_page_router.go（同一入口调用，装配顺序不变）。
	SetupInventoryPages(pages, svc, project)

	return svc
}

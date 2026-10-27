// purchase_page_router.go — purchase 模块的后台**页面**路由。
//
// 与 API 路由分开：API 挂三层链（Session + CSRF + Casbin），页面挂 Session + CSRF +
// 权限上下文，页面权限经 adminui.PageAuthz 复用 API 权限点（一条页面一个读权限点）。
package purchasehttp

import (
	"github.com/gin-gonic/gin"

	adminui "go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	projectcontract "go_wp/internal/module/project/contract"
	purchasecontract "go_wp/internal/module/purchase/contract"
)

// 页面模板（成功与失败渲染同一张，模板名由路由装配持有）。
const (
	// SourcesPageTemplate 货源管理页。
	SourcesPageTemplate = "admin/purchase/sources.html"
	// PurchasesPageTemplate 采购入库页。
	PurchasesPageTemplate = "admin/purchase/purchases.html"
)

// pageCatalogConsumers 装配期登记的后台页面处理器（写一次，之后只读）。
var pageCatalogConsumers []variantCatalogConsumer

// variantCatalogConsumer 需要「SKU 候选」只读口的后台页面处理器。
type variantCatalogConsumer interface {
	setVariantCatalog(catalog purchasecontract.VariantCatalog)
}

// SetVariantCatalog 为已注册的后台页面补注 SKU 候选只读口。
//
// 为什么必须后置注入：商品模块的装配要库存契约，库存的装配又要商品契约，
// 而采购页的 SKU 下拉同时需要「商品侧变体清单 + 库存侧裸码映射」——
// 这两条只读口在采购页注册的那一刻都还拿不到。与本仓其他装配期注入端口同一手法。
// 未注入时页面照常渲染，只是下拉为空（handler 侧对 nil 做降级，不 panic）。
func SetVariantCatalog(catalog purchasecontract.VariantCatalog) {
	for _, consumer := range pageCatalogConsumers {
		consumer.setVariantCatalog(catalog)
	}
}

// SetupPurchasePages 注册采购域后台页面；pages 为 nil 时整体跳过。
func SetupPurchasePages(pages *gin.RouterGroup, svc purchasecontract.PurchaseService,
	stock purchasecontract.StockPort, project projectcontract.ProjectService) {
	if pages == nil {
		return
	}
	// SKU 候选只在装配收尾补注，见 SetVariantCatalog。
	sourcePages := NewSourcePageHandle(svc, project)
	purchases := NewPurchasePageHandle(svc, stock, project)
	pageCatalogConsumers = append(pageCatalogConsumers, purchases)

	// 货源管理页：类型与关联方两个结构化维度支撑报表区分。
	pages.GET("/inventory/sources", adminui.PageAuthz("/api/inventory/source/list"),
		adminui.PageSlot(SourcesPageTemplate), sourcePages.SourcesPage)
	pages.POST("/inventory/sources/create", builtin.CasbinMiddlewareForPath("/api/inventory/source/create"), sourcePages.SourceCreate)
	pages.POST("/inventory/sources/update", builtin.CasbinMiddlewareForPath("/api/inventory/source/update"), sourcePages.SourceUpdate)
	pages.POST("/inventory/sources/delete", builtin.CasbinMiddlewareForPath("/api/inventory/source/delete"), sourcePages.SourceDelete)
	pages.POST("/inventory/sources/bulk-delete", builtin.CasbinMiddlewareForPath("/api/inventory/source/delete"), sourcePages.SourcesBulkDelete)

	// 采购入库页：采购单 → 逐行收货入库（经 StockPort 由库存落账）+ 生产入库。
	pages.GET("/inventory/purchases", adminui.PageAuthz("/api/inventory/purchase/list"),
		adminui.PageSlot(PurchasesPageTemplate), purchases.PurchasesPage)
	pages.POST("/inventory/purchases/create", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/create"), purchases.PurchaseCreate)
	pages.POST("/inventory/purchases/update", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchases.PurchaseUpdate)
	pages.POST("/inventory/purchases/receipt", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/receipt"), purchases.PurchaseReceipt)
	pages.POST("/inventory/purchases/production", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/production"), purchases.PurchaseProduction)
	// 采购单终态：提前关单 / 取消。权限点复用采购单更新 ——
	// 关单是一次对采购单的改写，不是新能力，不该多一个权限点。
	pages.POST("/inventory/purchases/finish", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchases.PurchaseFinish)
	pages.POST("/inventory/purchases/cancel", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchases.PurchaseCancel)
	// 670 状态机的另外三个动作：草稿下单 / 标异常 / 解除异常。
	// 权限点同样复用采购单更新：三个都是对**同一张已知采购单**的状态推进，
	// 不是新的资源能力；各拆一个权限点只会让「谁能推进单据」这件事散成五处判断。
	pages.POST("/inventory/purchases/place", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchases.PurchasePlace)
	pages.POST("/inventory/purchases/exception", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchases.PurchaseException)
	pages.POST("/inventory/purchases/resume", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchases.PurchaseResume)
}

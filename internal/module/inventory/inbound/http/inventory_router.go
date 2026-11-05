package inventoryhttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/config"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	inventoryservice "go_wp/internal/module/inventory/service"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

type productCatalogConsumer interface {
	setProductCatalog(products productcontract.VariantOptionsReader)
}

var pageCatalogConsumers []productCatalogConsumer

func SetProductCatalog(products productcontract.ProductService) {

	catalog, ok := products.(productcontract.VariantOptionsReader)
	if !ok {
		panic("inventory.SetProductCatalog: 商品契约未实现 VariantOptionsReader（两处接口不同步）")
	}
	for _, consumer := range pageCatalogConsumers {
		consumer.setProductCatalog(catalog)
	}
}

func SetupInventoryRoutes(rg *mw.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	project projectcontract.ProjectService) inventorycontract.InventoryService {
	svc := inventoryservice.NewService(inventorymodel.NewModel(db), project)

	if v, err := config.GetViper(); err == nil && v != nil {
		svc.SetCipherSecret(v.GetString("app.secret"))
	}
	handle := NewHandle(svc)

	g := rg.Group("/inventory")

	g.GET("/warehouse/list", inventoryenums.InventoryWarehouseList, handle.ListWarehouses)
	g.GET("/warehouse/get", inventoryenums.InventoryWarehouseGet, handle.GetWarehouse)
	g.POST("/warehouse/create", inventoryenums.InventoryWarehouseCreate, handle.CreateWarehouse)
	g.POST("/warehouse/update", inventoryenums.InventoryWarehouseUpdate, handle.UpdateWarehouse)
	g.POST("/warehouse/delete", inventoryenums.InventoryWarehouseDelete, handle.DeleteWarehouse)

	g.GET("/stock/list", inventoryenums.InventoryStockList, handle.ListStocks)
	g.GET("/stock/sku", inventoryenums.InventoryStockSKU, handle.ListStocksBySKU)
	g.GET("/stock/get", inventoryenums.InventoryStockGet, handle.GetStock)
	g.POST("/stock/ensure", inventoryenums.InventoryStockEnsure, handle.EnsureStock)

	g.GET("/stock/alert", inventoryenums.InventoryStockAlert, handle.StockAlert)

	g.POST("/stock/change", inventoryenums.InventoryStockChange, handle.ChangeStock)
	g.POST("/stock/deduct", inventoryenums.InventoryStockDeduct, handle.DeductStock)
	g.GET("/movement/list", inventoryenums.InventoryMovementList, handle.ListMovements)
	g.GET("/reason/list", inventoryenums.InventoryReasonList, handle.ListReasons)
	g.POST("/reason/create", inventoryenums.InventoryReasonCreate, handle.CreateReason)
	g.POST("/reason/update", inventoryenums.InventoryReasonUpdate, handle.UpdateReason)

	transferPages := NewInventoryTransferPageHandle(svc, project, nil)
	tg := rg.Group("/inventory/transfer")
	tg.GET("/list", inventoryenums.InventoryTransferList, transferPages.InventoryTransferListJSON)
	tg.GET("/get", inventoryenums.InventoryTransferGet, transferPages.InventoryTransferDetail)

	tg.GET("/stock-options", inventoryenums.InventoryTransferOptions, transferPages.InventoryTransferStockOptions)
	tg.POST("/create", inventoryenums.InventoryTransferCreate, pageview.HTMLFragment, transferPages.CreateTransfer)
	tg.POST("/approve", inventoryenums.InventoryTransferApprove, pageview.HTMLFragment, transferPages.ApproveTransfer)
	tg.POST("/reject", inventoryenums.InventoryTransferReject, pageview.HTMLFragment, transferPages.RejectTransfer)
	tg.POST("/ship", inventoryenums.InventoryTransferShip, pageview.HTMLFragment, transferPages.ShipTransfer)
	tg.POST("/receive", inventoryenums.InventoryTransferReceive, pageview.HTMLFragment, transferPages.ReceiveTransfer)
	tg.POST("/cancel", inventoryenums.InventoryTransferCancel, pageview.HTMLFragment, transferPages.CancelTransfer)

	SetupInventoryPages(pages, svc, project)

	return svc
}

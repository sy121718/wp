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

func SetupInventoryRoutes(rg *permission.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	project projectcontract.ProjectService) inventorycontract.InventoryService {
	svc := inventoryservice.NewService(inventorymodel.NewModel(db), project)

	if v, err := config.GetViper(); err == nil && v != nil {
		svc.SetCipherSecret(v.GetString("app.secret"))
	}
	handle := NewHandle(svc)

	g := rg.Group("/inventory")

	g.GET("/warehouse/list", permission.InventoryWarehouseList, handle.ListWarehouses)
	g.GET("/warehouse/get", permission.InventoryWarehouseGet, handle.GetWarehouse)
	g.POST("/warehouse/create", permission.InventoryWarehouseCreate, handle.CreateWarehouse)
	g.POST("/warehouse/update", permission.InventoryWarehouseUpdate, handle.UpdateWarehouse)
	g.POST("/warehouse/delete", permission.InventoryWarehouseDelete, handle.DeleteWarehouse)

	g.GET("/stock/list", permission.InventoryStockList, handle.ListStocks)
	g.GET("/stock/sku", permission.InventoryStockSKU, handle.ListStocksBySKU)
	g.GET("/stock/get", permission.InventoryStockGet, handle.GetStock)
	g.POST("/stock/ensure", permission.InventoryStockEnsure, handle.EnsureStock)

	g.GET("/stock/alert", permission.InventoryStockAlert, handle.StockAlert)

	g.POST("/stock/change", permission.InventoryStockChange, handle.ChangeStock)
	g.POST("/stock/deduct", permission.InventoryStockDeduct, handle.DeductStock)
	g.GET("/movement/list", permission.InventoryMovementList, handle.ListMovements)
	g.GET("/reason/list", permission.InventoryReasonList, handle.ListReasons)
	g.POST("/reason/create", permission.InventoryReasonCreate, handle.CreateReason)
	g.POST("/reason/update", permission.InventoryReasonUpdate, handle.UpdateReason)

	transferPages := NewInventoryTransferPageHandle(svc, project, nil)
	tg := rg.Group("/inventory/transfer")
	tg.GET("/list", permission.InventoryTransferList, transferPages.InventoryTransferListJSON)
	tg.GET("/get", permission.InventoryTransferGet, transferPages.InventoryTransferDetail)

	tg.GET("/stock-options", permission.InventoryTransferOptions, transferPages.InventoryTransferStockOptions)
	tg.POST("/create", permission.InventoryTransferCreate, pageview.HTMLFragment, transferPages.CreateTransfer)
	tg.POST("/approve", permission.InventoryTransferApprove, pageview.HTMLFragment, transferPages.ApproveTransfer)
	tg.POST("/reject", permission.InventoryTransferReject, pageview.HTMLFragment, transferPages.RejectTransfer)
	tg.POST("/ship", permission.InventoryTransferShip, pageview.HTMLFragment, transferPages.ShipTransfer)
	tg.POST("/receive", permission.InventoryTransferReceive, pageview.HTMLFragment, transferPages.ReceiveTransfer)
	tg.POST("/cancel", permission.InventoryTransferCancel, pageview.HTMLFragment, transferPages.CancelTransfer)

	SetupInventoryPages(pages, svc, project)

	return svc
}

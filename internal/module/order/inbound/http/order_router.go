package orderhttp

import (
	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/response"
	"gorm.io/gorm"
	"time"

	ordercontract "go_wp/internal/module/order/contract"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/internal/module/order/outbound/currencydict"
	orderservice "go_wp/internal/module/order/service"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	usercontract "go_wp/internal/module/user/contract"
	webhookcontract "go_wp/internal/module/webhook/contract"

	"go_wp/internal/permission"
	pageview "go_wp/pkg/view"
)

func SetupOrderRoutes(rg *permission.RouteGroup,
	db *gorm.DB,

	product productcontract.ProductService,
	stock ordercontract.StockOperator,
	guest usercontract.GuestAccountProvisioner,

	webhooks webhookcontract.Dispatcher,

	projects projectcontract.ProjectService,

	pages *gin.RouterGroup,

	warehouses ordercontract.ReturnWarehouseSource,

	dict sysconfigcontract.DictReader,
) ordercontract.OrderService {
	svc := orderservice.NewService(
		ordermodel.NewOrderModel(db),
		ordermodel.NewOrderItemModel(db),
		ordermodel.NewOrderStatusLogModel(db),

		ordermodel.NewCouponModel(db),

		ordermodel.NewReturnModel(db),
		product,
		stock,
		guest,
		webhooks,
	)

	svc.SetProjects(projects)

	svc.SetShipmentModels(ordermodel.NewOrderShipmentModel(db), ordermodel.NewOrderCarrierModel(db))

	if warehouses != nil {
		svc.SetWarehouseSource(warehouses)
	}

	if dict != nil {
		svc.SetCurrencyLister(currencydict.New(dict))
	}
	h := NewHandle(svc)

	orderservice.StartPendingOrderExpiryScheduler(svc)

	g := rg.Group("/order")

	g.GET("/list", permission.OrderList, h.ListOrders)
	g.GET("/get", permission.OrderGet, h.GetOrder)
	g.GET("/item/list", permission.OrderItemList, h.ListItems)
	g.GET("/log/list", permission.OrderLogList, h.ListLogs)

	g.POST("/create", permission.OrderCreate, pageview.HTMLFragment, h.CreateOrder)
	g.POST("/status", permission.OrderStatus, pageview.HTMLFragment, h.ChangeStatus)
	g.POST("/cancel", permission.OrderCancel, pageview.HTMLFragment, h.CancelOrder)
	g.POST("/refund", permission.OrderRefund, pageview.HTMLFragment, h.RefundOrder)

	g.POST("/note", permission.OrderNote, pageview.HTMLFragment, h.UpdateOrderNote)

	cg := rg.Group("/order/coupon")
	cg.GET("/list", permission.OrderCouponList, h.ListCoupons)
	cg.GET("/get", permission.OrderCouponGet, h.GetCoupon)
	cg.GET("/validate", permission.OrderCouponValidate, h.ValidateCoupon)
	cg.GET("/redemption/list", permission.OrderCouponRedemption, h.ListCouponRedemptions)

	cg.GET("/count-audit", permission.OrderCouponCountAudit, h.AuditCouponCounts)
	cg.POST("/create", permission.OrderCouponCreate, pageview.HTMLFragment, h.CreateCoupon)
	cg.POST("/update", permission.OrderCouponUpdate, pageview.HTMLFragment, h.UpdateCoupon)
	cg.POST("/delete", permission.OrderCouponDelete, pageview.HTMLFragment, h.DeleteCoupon)

	sg := rg.Group("/order/shipment")
	sg.GET("/list", permission.OrderShipmentList, h.ListShipments)
	sg.GET("/pending", permission.OrderPendingShip, h.ListPendingShip)
	sg.GET("/form", permission.OrderShipForm, h.ShipForm)
	sg.POST("/create", permission.OrderShip, pageview.HTMLFragment, h.ShipOrder)

	carg := rg.Group("/order/carrier")
	carg.GET("/list", permission.OrderCarrierList, h.ListCarriers)
	carg.POST("/create", permission.OrderCarrierCreate, pageview.HTMLFragment, h.CreateCarrier)
	carg.POST("/update", permission.OrderCarrierManage, pageview.HTMLFragment, h.UpdateCarrier)

	rg.GET("/order/currency/rate", permission.OrderCurrencyRateGet, h.GetCurrencyRate)
	rg.POST("/order/currency/rate", permission.OrderCurrencyRateSet, pageview.HTMLFragment, h.SetCurrencyRate)

	rgp := rg.Group("/order/return")
	rgp.GET("/list", permission.OrderReturnList, h.ListReturns)
	rgp.GET("/get", permission.OrderReturnGet, h.GetReturn)
	rgp.POST("/approve", permission.OrderReturnApprove, pageview.HTMLFragment, h.ApproveReturn)
	rgp.POST("/reject", permission.OrderReturnReject, pageview.HTMLFragment, h.RejectReturn)
	rgp.POST("/receive", permission.OrderReturnReceive, pageview.HTMLFragment, h.ReceiveReturn)

	SetupOrderPages(pages, svc, projects, product, warehouses, dict)

	return svc
}

func SetupOrderPublicRoutes(router *gin.Engine, svc ordercontract.VisitorOrderPort, middleware ...gin.HandlerFunc) {
	if router == nil || svc == nil {
		return
	}
	publicAPI := permission.NewRouteGroup(router.Group("/api/site/orders", middleware...))
	publicAPI.Use(builtin.RequestRateLimitMiddleware(90, time.Minute), requireVisitorOrderUser())
	h := &PublicHandle{svc: svc}
	publicAPI.GET("", permission.Exempt, h.VisitorList)
	publicAPI.GET("/detail", permission.Exempt, h.VisitorDetail)
	publicAPI.GET("/returnable", permission.Exempt, h.VisitorReturnable)
	publicAPI.GET("/returns", permission.Exempt, h.VisitorReturns)
	publicAPI.POST("/returns", permission.Exempt, h.VisitorRequestReturn)
}
func requireVisitorOrderUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if visitorOrderUserID(c) == 0 {
			response.ErrorStatusAuto(c, "user", userenums.Err(userenums.ErrNotLoggedIn))
			c.Abort()
			return
		}
		c.Next()
	}
}

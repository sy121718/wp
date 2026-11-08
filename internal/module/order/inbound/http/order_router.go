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

	orderenums "go_wp/internal/module/order/enums"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

func SetupOrderRoutes(rg *mw.RouteGroup,
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

	g.GET("/list", orderenums.OrderList, h.ListOrders)
	g.GET("/get", orderenums.OrderGet, h.GetOrder)
	g.GET("/item/list", orderenums.OrderItemList, h.ListItems)
	g.GET("/log/list", orderenums.OrderLogList, h.ListLogs)

	g.POST("/create", orderenums.OrderCreate, pageview.HTMLFragment, h.CreateOrder)
	g.POST("/status", orderenums.OrderStatus, pageview.HTMLFragment, h.ChangeStatus)
	g.POST("/cancel", orderenums.OrderCancel, pageview.HTMLFragment, h.CancelOrder)
	g.POST("/refund", orderenums.OrderRefund, pageview.HTMLFragment, h.RefundOrder)

	g.POST("/note", orderenums.OrderNote, pageview.HTMLFragment, h.UpdateOrderNote)

	cg := rg.Group("/order/coupon")
	cg.GET("/list", orderenums.OrderCouponList, h.ListCoupons)
	cg.GET("/get", orderenums.OrderCouponGet, h.GetCoupon)
	cg.GET("/validate", orderenums.OrderCouponValidate, h.ValidateCoupon)
	cg.GET("/redemption/list", orderenums.OrderCouponRedemption, h.ListCouponRedemptions)

	cg.GET("/count-audit", orderenums.OrderCouponCountAudit, h.AuditCouponCounts)
	cg.POST("/create", orderenums.OrderCouponCreate, pageview.HTMLFragment, h.CreateCoupon)
	cg.POST("/update", orderenums.OrderCouponUpdate, pageview.HTMLFragment, h.UpdateCoupon)
	cg.POST("/delete", orderenums.OrderCouponDelete, pageview.HTMLFragment, h.DeleteCoupon)

	sg := rg.Group("/order/shipment")
	sg.GET("/list", orderenums.OrderShipmentList, h.ListShipments)
	sg.GET("/pending", orderenums.OrderPendingShip, h.ListPendingShip)
	sg.GET("/form", orderenums.OrderShipForm, h.ShipForm)
	sg.POST("/create", orderenums.OrderShip, pageview.HTMLFragment, h.ShipOrder)

	carg := rg.Group("/order/carrier")
	carg.GET("/list", orderenums.OrderCarrierList, h.ListCarriers)
	carg.POST("/create", orderenums.OrderCarrierCreate, pageview.HTMLFragment, h.CreateCarrier)
	carg.POST("/update", orderenums.OrderCarrierManage, pageview.HTMLFragment, h.UpdateCarrier)

	rg.GET("/order/currency/rate", orderenums.OrderCurrencyRateGet, h.GetCurrencyRate)
	rg.POST("/order/currency/rate", orderenums.OrderCurrencyRateSet, pageview.HTMLFragment, h.SetCurrencyRate)

	rgp := rg.Group("/order/return")
	rgp.GET("/list", orderenums.OrderReturnList, h.ListReturns)
	rgp.GET("/get", orderenums.OrderReturnGet, h.GetReturn)
	rgp.POST("/approve", orderenums.OrderReturnApprove, pageview.HTMLFragment, h.ApproveReturn)
	rgp.POST("/reject", orderenums.OrderReturnReject, pageview.HTMLFragment, h.RejectReturn)
	rgp.POST("/receive", orderenums.OrderReturnReceive, pageview.HTMLFragment, h.ReceiveReturn)

	SetupOrderPages(rg.Registry(), pages, svc, projects, product, warehouses, dict)

	return svc
}

func SetupOrderPublicRoutes(reg *mw.Registry, router *gin.Engine, svc ordercontract.VisitorOrderPort, middleware ...gin.HandlerFunc) {
	if router == nil || svc == nil {
		return
	}
	publicAPI := mw.NewRouteGroupIn(reg, router.Group("/api/site/orders", middleware...), mw.RouteExempt)
	publicAPI.Use(builtin.RequestRateLimitMiddleware(90, time.Minute), requireVisitorOrderUser())
	h := &PublicHandle{svc: svc}
	publicAPI.GET("", "", h.VisitorList)
	publicAPI.GET("/detail", "", h.VisitorDetail)
	publicAPI.GET("/returnable", "", h.VisitorReturnable)
	publicAPI.GET("/returns", "", h.VisitorReturns)
	publicAPI.POST("/returns", "", h.VisitorRequestReturn)
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

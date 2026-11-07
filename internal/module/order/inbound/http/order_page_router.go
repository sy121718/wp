package orderhttp

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	ordercontract "go_wp/internal/module/order/contract"
	orderenums "go_wp/internal/module/order/enums"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

const (
	PendingShipPageTemplate   = "admin/order/order_pending_ship.html"
	ShipFormPageTemplate      = "admin/order/order_ship_form.html"
	OrderCarriersPageTemplate = "admin/order/order_carriers.html"
)

var salesPageAlertSinks []*orderSalesPageHandle

func registerSalesPageAlerts(h *orderSalesPageHandle) {
	salesPageAlertSinks = append(salesPageAlertSinks, h)
}

func SetSalesStockAlerts(fn func(ctx context.Context, projectID string) (int, error)) {
	for _, h := range salesPageAlertSinks {
		h.SetStockAlerts(fn)
	}
}

const OrderNewPageTemplate = "admin/order/order_new.html"

const ReturnListPageTemplate = "admin/order/returns.html"

const CouponListPageTemplate = "admin/order/coupons.html"

const CouponEditFormTemplate = "admin/order/coupon_edit_form.html"

const OrderListPageTemplate = "admin/order/orders.html"

const OrderSalesPageTemplate = "admin/order/sales_overview.html"

func SetupOrderPages(pages *gin.RouterGroup, svc ordercontract.OrderService,
	projects projectcontract.ProjectService, product productcontract.ProductService,
	warehouses ordercontract.ReturnWarehouseSource, dict sysconfigcontract.DictReader) {
	if pages == nil {
		return
	}

	orderPages := NewOrderPageHandle(svc, projects, dict)
	pages.GET("/orders",
		mw.PageAuthz("/api/order/list"),
		pageview.UseRenderer(pageview.RenderPage),
		orderPages.OrdersPage)

	salesPages := NewOrderSalesPageHandle(svc, projects)

	if shipmentCounter, ok := svc.(orderShipmentCounter); ok {
		salesPages.SetShipmentCounter(shipmentCounter)
	}

	registerSalesPageAlerts(salesPages)

	pages.GET("/orders/overview",
		mw.PageAuthz("/admin/orders/overview"),
		pageview.UseRenderer(pageview.RenderPage),
		salesPages.SalesOverviewPage)

	mw.DeclareRoute(mw.RouteSpec{
		Method: http.MethodGet, Path: "/admin/orders/overview",
		Perm: orderenums.OrderOverview, Kind: mw.RoutePage,
		AuthObject: "/admin/orders/overview", AuthMethod: http.MethodGet,
	})

	orderCreate := NewOrderCreatePageHandle(svc, projects, product, dict)
	pages.GET("/orders/new",
		mw.PageAuthz("/api/order/list"),
		pageview.UseRenderer(pageview.RenderPage),
		orderCreate.OrderCreatePage)

	pages.POST("/orders/create",
		builtin.CasbinMiddlewareForPath("/api/order/create"),
		pageview.HTMLFragment,
		orderCreate.OrderCreateSubmit)

	returnPages := NewReturnPageHandle(svc, projects, warehouses, dict)
	pages.GET("/returns",
		mw.PageAuthz("/api/order/return/list"),
		pageview.UseRenderer(pageview.RenderPage),
		returnPages.ReturnsPage)

	shipPages := NewOrderShipmentPageHandle(svc, projects)
	pages.GET("/orders/pending-ship", mw.PageAuthz("/api/order/shipment/pending"),
		pageview.UseRenderer(pageview.RenderPage), shipPages.PendingShipPage)

	pages.GET("/orders/ship-form", builtin.CasbinMiddlewareForPathAs("/api/order/shipment/create", http.MethodPost),
		pageview.UseRenderer(pageview.RenderBare), shipPages.ShipFormFragment)
	pages.GET("/order-carriers", mw.PageAuthz("/api/order/carrier/list"),
		pageview.UseRenderer(pageview.RenderPage), shipPages.OrderCarriersPage)

	mw.DeclareRoute(mw.RouteSpec{
		Method: http.MethodGet, Path: "/admin/orders/pending-ship",
		Perm: orderenums.OrderPendingShip, Kind: mw.RoutePage,
		AuthObject: "/api/order/shipment/pending", AuthMethod: http.MethodGet,
	})
	mw.DeclareRoute(mw.RouteSpec{
		Method: http.MethodGet, Path: "/admin/order-carriers",
		Perm: orderenums.OrderCarrierList, Kind: mw.RoutePage,
		AuthObject: "/api/order/carrier/list", AuthMethod: http.MethodGet,
	})

	couponPages := NewCouponPageHandle(svc, projects)
	pages.GET("/coupons",
		mw.PageAuthz("/api/order/coupon/list"),
		pageview.UseRenderer(pageview.RenderPage),
		couponPages.CouponsPage)
	pages.GET("/coupons/edit-form",
		builtin.CasbinMiddlewareForPathAs("/api/order/coupon/update", http.MethodPost),
		pageview.UseRenderer(pageview.RenderBare),
		couponPages.CouponEditForm)
}

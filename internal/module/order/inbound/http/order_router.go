// order_router.go — 订单模块路由自装配（BIZ-1 销售侧）。
//
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin），与其它后台模块一致。
// 访客侧下单是另一条路径（走 Runtime Fragment），不在这里。
package orderhttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	ordercontract "go_wp/internal/module/order/contract"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	productcontract "go_wp/internal/module/product/contract"
	usercontract "go_wp/internal/module/user/contract"
)

// SetupOrderRoutes 装配订单模块路由，返回模块契约。
//
// product 提供下单快照（只读），stock 提供扣减与归还两条库存能力 ——
// 两者都是收窄过的接口，不是各自模块的完整 Service。
func SetupOrderRoutes(
	rg *gin.RouterGroup,
	db *gorm.DB,
	product productcontract.VariantSnapshotPort,
	stock ordercontract.StockOperator,
	guest usercontract.GuestAccountProvisioner,
) ordercontract.OrderService {
	svc := orderservice.NewService(
		ordermodel.NewOrderModel(db),
		ordermodel.NewOrderItemModel(db),
		ordermodel.NewOrderStatusLogModel(db),
		// 优惠码与订单同模块：核销要和建单落在同一个事务里，
		// 跨模块事务在这里是不允许的，所以它必须是本模块的 model。
		ordermodel.NewCouponModel(db),
		product,
		stock,
		guest,
	)
	h := NewHandle(svc)

	g := rg.Group("/order")
	// 查询
	g.GET("/list", h.ListOrders)
	g.GET("/get", h.GetOrder)
	g.GET("/item/list", h.ListItems)
	g.GET("/log/list", h.ListLogs)
	// 写入
	g.POST("/create", h.CreateOrder)
	g.POST("/status", h.ChangeStatus)
	g.POST("/cancel", h.CancelOrder)
	g.POST("/refund", h.RefundOrder)

	// 优惠码（BIZ-1）：管理 + 试算。核销不在这里 —— 它在建单事务内完成。
	cg := rg.Group("/order/coupon")
	cg.GET("/list", h.ListCoupons)
	cg.GET("/get", h.GetCoupon)
	cg.GET("/validate", h.ValidateCoupon)
	cg.GET("/redemption/list", h.ListCouponRedemptions)
	cg.POST("/create", h.CreateCoupon)
	cg.POST("/update", h.UpdateCoupon)
	cg.POST("/delete", h.DeleteCoupon)

	return svc
}

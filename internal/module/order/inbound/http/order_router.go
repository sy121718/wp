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
	webhookcontract "go_wp/internal/module/webhook/contract"
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
	// webhooks 外部集成派发口（OSS-006）：支付落账后向登记的端点派发 order.paid。
	// 只取 DispatchEvent 一条能力（收窄端口），订单看不到端点配置与投递日志。
	webhooks webhookcontract.Dispatcher,
) ordercontract.OrderService {
	svc := orderservice.NewService(
		ordermodel.NewOrderModel(db),
		ordermodel.NewOrderItemModel(db),
		ordermodel.NewOrderStatusLogModel(db),
		// 优惠码与订单同模块：核销要和建单落在同一个事务里，
		// 跨模块事务在这里是不允许的，所以它必须是本模块的 model。
		ordermodel.NewCouponModel(db),
		// 退货聚合（单头 + 明细）同理：建单头与写明细要同生共死。
		ordermodel.NewReturnModel(db),
		product,
		stock,
		guest,
		webhooks,
	)
	h := NewHandle(svc)
	// 待付款超时自动取消（TX-001）：进程内定时扫描，失败不阻断启动。
	orderservice.StartPendingOrderExpiryScheduler(svc)

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
	// 后台备注：只改一列，不写状态流转（备注不是状态变化）。
	g.POST("/note", h.UpdateOrderNote)

	// 优惠码（BIZ-1）：管理 + 试算。核销不在这里 —— 它在建单事务内完成。
	cg := rg.Group("/order/coupon")
	cg.GET("/list", h.ListCoupons)
	cg.GET("/get", h.GetCoupon)
	cg.GET("/validate", h.ValidateCoupon)
	cg.GET("/redemption/list", h.ListCouponRedemptions)
	// 券计数对账（DB-021）：只读巡检，used_count 是投影、核销明细是真源。
	cg.GET("/count-audit", h.AuditCouponCounts)
	cg.POST("/create", h.CreateCoupon)
	cg.POST("/update", h.UpdateCoupon)
	cg.POST("/delete", h.DeleteCoupon)

	// 退货入库（RMA）：客户在访问面提交申请，后台在这里审核与收货。
	// **先入库、后退款**的强顺序由 service 保证（见 return_review.go）。
	rgp := rg.Group("/order/return")
	rgp.GET("/list", h.ListReturns)
	rgp.GET("/get", h.GetReturn)
	rgp.POST("/approve", h.ApproveReturn)
	rgp.POST("/reject", h.RejectReturn)
	rgp.POST("/receive", h.ReceiveReturn)

	return svc
}

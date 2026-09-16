// order_router.go — 订单模块路由自装配（BIZ-1 销售侧）。
//
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin），与其它后台模块一致。
// 访客侧下单是另一条路径（走 Runtime Fragment），不在这里。
package orderhttp

import (
	"gorm.io/gorm"

	ordercontract "go_wp/internal/module/order/contract"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"
	webhookcontract "go_wp/internal/module/webhook/contract"
	"go_wp/internal/permission"
)

// SetupOrderRoutes 装配订单模块路由，返回模块契约。
//
// product 提供下单快照（只读），stock 提供扣减与归还两条库存能力 ——
// 两者都是收窄过的接口，不是各自模块的完整 Service。
func SetupOrderRoutes(rg *permission.RouteGroup,
	db *gorm.DB,
	product productcontract.VariantSnapshotPort,
	stock ordercontract.StockOperator,
	guest usercontract.GuestAccountProvisioner,
	// webhooks 外部集成派发口（OSS-006）：支付落账后向登记的端点派发 order.paid。
	// 只取 DispatchEvent 一条能力（收窄端口），订单看不到端点配置与投递日志。
	webhooks webhookcontract.Dispatcher,
	// projects 工程契约：只用于列出工程 id，给「只带 id」的入口逐工程探测归属。
	// 未注入时退回读 projects 表的兜底路径（本模块唯一的跨表读取），故装配点必须注入。
	projects projectcontract.ProjectService,
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
	// 只带 id 的入口（取消 / 改状态 / 退款 / 备注 / 超时扫描）靠它逐工程定位归属，
	// 不注入会退回读 projects 表的兜底路径。
	svc.SetProjects(projects)
	h := NewHandle(svc)
	// 待付款超时自动取消（TX-001）：进程内定时扫描，失败不阻断启动。
	orderservice.StartPendingOrderExpiryScheduler(svc)

	g := rg.Group("/order")
	// 查询
	g.GET("/list", permission.OrderList, h.ListOrders)
	g.GET("/get", permission.OrderGet, h.GetOrder)
	g.GET("/item/list", permission.OrderItemList, h.ListItems)
	g.GET("/log/list", permission.OrderLogList, h.ListLogs)
	// 写入
	g.POST("/create", permission.OrderCreate, h.CreateOrder)
	g.POST("/status", permission.OrderStatus, h.ChangeStatus)
	g.POST("/cancel", permission.OrderCancel, h.CancelOrder)
	g.POST("/refund", permission.OrderRefund, h.RefundOrder)
	// 后台备注：只改一列，不写状态流转（备注不是状态变化）。
	g.POST("/note", permission.OrderNote, h.UpdateOrderNote)

	// 优惠码（BIZ-1）：管理 + 试算。核销不在这里 —— 它在建单事务内完成。
	cg := rg.Group("/order/coupon")
	cg.GET("/list", permission.OrderCouponList, h.ListCoupons)
	cg.GET("/get", permission.OrderCouponGet, h.GetCoupon)
	cg.GET("/validate", permission.OrderCouponValidate, h.ValidateCoupon)
	cg.GET("/redemption/list", permission.OrderCouponRedemption, h.ListCouponRedemptions)
	// 券计数对账（DB-021）：只读巡检，used_count 是投影、核销明细是真源。
	cg.GET("/count-audit", permission.OrderCouponCountAudit, h.AuditCouponCounts)
	cg.POST("/create", permission.OrderCouponCreate, h.CreateCoupon)
	cg.POST("/update", permission.OrderCouponUpdate, h.UpdateCoupon)
	cg.POST("/delete", permission.OrderCouponDelete, h.DeleteCoupon)

	// 退货入库（RMA）：客户在访问面提交申请，后台在这里审核与收货。
	// **先入库、后退款**的强顺序由 service 保证（见 return_review.go）。
	rgp := rg.Group("/order/return")
	rgp.GET("/list", permission.OrderReturnList, h.ListReturns)
	rgp.GET("/get", permission.OrderReturnGet, h.GetReturn)
	rgp.POST("/approve", permission.OrderReturnApprove, h.ApproveReturn)
	rgp.POST("/reject", permission.OrderReturnReject, h.RejectReturn)
	rgp.POST("/receive", permission.OrderReturnReceive, h.ReceiveReturn)

	return svc
}

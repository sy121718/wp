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
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
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
	// product 商品目录契约。下单快照用它的 VariantSnapshotPort 那一半（只读），
	// 后台代客建单页的候选 SKU 用它的 ListBundleSKUs（跨商品列启用变体）。
	// 参数取全量契约而不再是单独的 VariantSnapshotPort：ProductService **天然嵌入**
	// 那个端口，所以装配点（routers/assembly.go）传的实参一个字都不用改 ——
	// 为一条只读能力新增一个装配参数，等于让所有调用方跟着改一遍。
	product productcontract.ProductService,
	stock ordercontract.StockOperator,
	guest usercontract.GuestAccountProvisioner,
	// webhooks 外部集成派发口（OSS-006）：支付落账后向登记的端点派发 order.paid。
	// 只取 DispatchEvent 一条能力（收窄端口），订单看不到端点配置与投递日志。
	webhooks webhookcontract.Dispatcher,
	// projects 工程契约：只用于列出工程 id，给「只带 id」的入口逐工程探测归属。
	// 未注入时退回读 projects 表的兜底路径（本模块唯一的跨表读取），故装配点必须注入。
	projects projectcontract.ProjectService,
	// pages 后台页面组（前缀 /admin，装配层已挂 Session + CSRF + 权限上下文）。
	// 传 nil 时只注册 API，不注册后台页面 —— 与 rg 为空时的语义一致。
	pages *gin.RouterGroup,
	// warehouses 退货页「入库仓库」下拉的窄端口（ordercontract 自有视图类型，
	// 库存侧持有适配器 —— CQ-004 同一手法，订单模块不 import 库存的 dto）：
	// 退货入到哪个仓是运营的决定，让他手填仓库 id 是把内部标识当输入项，填错不报错、货就进错仓。
	warehouses ordercontract.ReturnWarehouseSource,
	// dict 系统字典只读口（sysconfig 的 DictReader）：订单详情 / 退货详情的订单摘要
	// 要把快照里的国家代码显示成当前语言的名字。**可选** —— 传 nil 时页面显示代码、
	// 不做任何降级处理（一个展示标签读不到，不该让整页失败）。
	dict sysconfigcontract.DictReader,
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
	// **先入库、后退款**的强顺序由 service 的 ReceiveReturn 保证。
	rgp := rg.Group("/order/return")
	rgp.GET("/list", permission.OrderReturnList, h.ListReturns)
	rgp.GET("/get", permission.OrderReturnGet, h.GetReturn)
	rgp.POST("/approve", permission.OrderReturnApprove, h.ApproveReturn)
	rgp.POST("/reject", permission.OrderReturnReject, h.RejectReturn)
	rgp.POST("/receive", permission.OrderReturnReceive, h.ReceiveReturn)

	// 后台页面：注册在 order_page_router.go（同一入口调用，装配顺序不变）。
	SetupOrderPages(pages, svc, projects, product, warehouses, dict)

	return svc
}

// Package ordercontract 订单模块对外契约（BIZ-1 销售侧）。
package ordercontract

import (
	"context"

	orderdto "go_wp/internal/module/order/dto"
	inventorydto "go_wp/internal/module/product/inventory/dto"
)

// OrderService 订单模块对外能力。
type OrderService interface {
	// VisitorOrderReader 访客自助查询（只有两条只读方法，user_id 钉死在契约里）。
	VisitorOrderReader
	// OrderNoReader 按商户单号取单（支付回调链路的第一步）。
	OrderNoReader
	// CouponService 优惠码：后台管理 + 结算试算。
	CouponService
	// ReturnService 退货入库（RMA）：客户申请 → 审核 → 先入库后退款。
	ReturnService

	// CreateOrder 建单：读商品事实落快照 → 扣库存 → 写订单（落在同一事务里）。
	CreateOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error)
	// GetOrder 订单详情（头 + 订单项 + 状态流转链）。
	GetOrder(ctx context.Context, orderID uint64) (res *orderdto.OrderDetailResp, err error)
	// ListOrders 订单列表 + 各状态计数。
	ListOrders(ctx context.Context, req *orderdto.ListOrderReq) (res *orderdto.OrderListResp, err error)
	// ChangeStatus 状态流转（哪条边合法由状态机判定）。
	ChangeStatus(ctx context.Context, req *orderdto.ChangeStatusReq) (err error)
	// PayOrder 支付落账：pending 推进到 paid，写 paid_at / 支付通道 / 支付流水号。
	//
	// 幂等：已经是 paid / shipped / completed 时原样返回成功且**不改任何列** ——
	// 网关重复通知、访客连点两次、失败重试都会走到这条路径上，
	// 把重复当错误会让对方无限重试。
	PayOrder(ctx context.Context, req *orderdto.PayOrderReq) (res *orderdto.PayOrderResp, err error)
	// CancelOrder 取消订单：归还库存 + 记流转。
	CancelOrder(ctx context.Context, req *orderdto.CancelOrderReq) (err error)
	// UpdateOrderNote 改订单的后台备注（adminNote）。
	//
	// 备注不是状态流转：它不改变订单处在哪一步，因此不写 status_logs ——
	// 混进流转链会让「这单什么时候发的货」变成要翻记录才能看出来。
	UpdateOrderNote(ctx context.Context, req *orderdto.UpdateOrderNoteReq) (res *orderdto.OrderResp, err error)
	// RefundOrder 退款：改状态 + 记流水号。
	//
	// **不归还库存** —— 退款是钱的事，退货入库是货的事，两者可以不同步
	// （比如只退运费、或有质量问题直接退款不退货）。合并成一步会让「只退款」
	// 这种正常诉求没法表达。
	RefundOrder(ctx context.Context, req *orderdto.RefundOrderReq) (err error)
}

// VisitorReturnPort 访客侧的退货能力（访问面片段层消费的**收窄面**）。
//
// 为什么单独成接口：片段层不该拿到「后台审核 / 入库 / 退款」那几条 ——
// 越权防护靠接口形状，而不是靠调用方自觉。三条方法都以 userID 收口，
// 调用方没有「不传身份」这个选项。
type VisitorReturnPort interface {
	// RequestReturn 提交退货申请（UserID 由片段层从会话写入）。
	RequestReturn(ctx context.Context, req *orderdto.ReturnRequestReq) (res *orderdto.ReturnResp, err error)
	// ReturnableOfOrder 该订单各订单项的当前可退数量（渲染表单用）。
	ReturnableOfOrder(ctx context.Context, req *orderdto.VisitorOrderDetailReq) (res *orderdto.ReturnableResp, err error)
	// ListVisitorReturns 访客查自己的退货申请。
	ListVisitorReturns(ctx context.Context, req *orderdto.VisitorReturnListReq) (res *orderdto.ReturnListResp, err error)
}

// ReturnService 退货申请（RMA）：客户申请 → 管理员审核 → **先入库、后退款**。
//
// 与 CancelOrder / RefundOrder 的分工：
//
//	· CancelOrder 是「货还没出去」，它直接归还库存；
//	· RefundOrder 只处理钱（契约里明写「不归还库存」）；
//	· 本接口把两者串起来，**但顺序不能反**：先入库（货真的回来了）再退款 ——
//	  反过来就是「钱退了、货没回来」，而这正是退货流程最容易被薅的地方。
//
// 部分退货是一等公民：按订单项记数量，「已退多少」由明细聚合算出（不存冗余计数 ——
// 冗余计数总有一天会与明细对不上，而对不上的时候没人知道该信哪一个）。
type ReturnService interface {
	// VisitorReturnPort 客户侧的三条（片段层只拿得到这些）。
	VisitorReturnPort

	// CancelReturn 客户撤销自己**尚未审核**的申请。
	CancelReturn(ctx context.Context, req *orderdto.ReturnCancelReq) (err error)

	// ListReturns 后台列表（含各状态计数）。
	ListReturns(ctx context.Context, req *orderdto.ReturnListReq) (res *orderdto.ReturnListResp, err error)
	// GetReturn 详情（含订单摘要与逐行**可退数量**）。
	GetReturn(ctx context.Context, returnID uint64) (res *orderdto.ReturnDetailResp, err error)
	// ApproveReturn 同意（AutoReceive=true 时一步完成入库 + 退款）。
	ApproveReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (res *orderdto.ReturnResp, err error)
	// RejectReturn 拒绝（必须给理由 —— 客户要知道为什么，否则他会再申请一次）。
	RejectReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (res *orderdto.ReturnResp, err error)
	// ReceiveReturn 确认收货：入库 + 退款。
	//
	// 幂等且**可重入**：入库成功后网络断了、再点一次只会补做没完成的那一步
	//（入库里有唯一来源引用与状态守卫，退款走幂等的 PayOrder/RefundOrder 口径）。
	ReceiveReturn(ctx context.Context, req *orderdto.ReturnReceiveReq) (res *orderdto.ReturnResp, err error)
}

// StockOperator 订单需要的库存能力 —— **只有扣减与增加这两条**。
//
// 为什么不直接依赖 inventorycontract.InventoryService：那个接口有二十来个方法
// （仓库 / 库存查询 / 流水 / 原因字典 / 物料清单 / 采购单 / 入库 / 进货历史），
// 订单一条都用不上。收窄的理由同 user 模块的 MailSender：依赖面越大，越容易在
// 不经意间用上不该用的能力；测试造替身时，二十个方法的空实现也会淹没测试意图。
//
// inventory 的 Service 天然满足这个接口（它有这两个方法），装配时直接传即可。
type StockOperator interface {
	// DeductStock 按 SKU 扣减库存：任一行不足即整体拒绝（不会扣一半）。
	DeductStock(ctx context.Context, req *inventorydto.DeductStockReq) (res *inventorydto.StockChangeResp, err error)
	// ChangeStock 按 SKU 增减库存：取消订单时用来归还。
	ChangeStock(ctx context.Context, req *inventorydto.ChangeStockReq) (res *inventorydto.StockChangeResp, err error)
}

// OrderNoReader 按商户单号取订单。
//
// 支付通道的异步回调只有商户单号（它不认识我们的自增 id），而 PayOrder 只接受内部 id ——
// 这是刻意的（见 PayOrderReq 注释），两者之间需要这一层翻译。
type OrderNoReader interface {
	GetOrderByNo(ctx context.Context, req *orderdto.GetOrderByNoReq) (res *orderdto.OrderResp, err error)
}

// VisitorOrderReader 访客自助查询自己订单的能力。
//
// 为什么不让访客直接调 ListOrders：那条路径返回全站状态计数、且 UserID 只是个可选过滤项 ——
// 「必须限定在自己名下」这件事交给调用方记得传，总有一天会有人忘。
// 这里两个方法把 user_id 钉进契约：调用方没有不传的选项，归属过滤写在 SQL 条件里。
type VisitorOrderReader interface {
	ListVisitorOrders(ctx context.Context, req *orderdto.VisitorOrderListReq) (res *orderdto.VisitorOrderListResp, err error)
	GetVisitorOrder(ctx context.Context, req *orderdto.VisitorOrderDetailReq) (res *orderdto.OrderDetailResp, err error)
}

// CouponService 优惠码能力。
//
// 试算（ValidateCoupon）是纯读、不占次数，供结算页在提交前先告诉访客能减多少；
// 真正的核销发生在 CreateOrder 的事务里（见 CreateOrderReq.CouponCode），**不单独暴露核销入口** ——
// 允许「先核销、后建单」的接口一定会被用出「券没了但没下单」这种状态。
type CouponService interface {
	CreateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (res *orderdto.CouponResp, err error)
	UpdateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (res *orderdto.CouponResp, err error)
	ListCoupons(ctx context.Context, req *orderdto.CouponListReq) (res *orderdto.CouponListResp, err error)
	GetCoupon(ctx context.Context, couponID uint64) (res *orderdto.CouponResp, err error)
	DeleteCoupon(ctx context.Context, couponID uint64) (err error)
	ValidateCoupon(ctx context.Context, req *orderdto.CouponValidateReq) (res *orderdto.CouponValidateResp, err error)
	ListCouponRedemptions(ctx context.Context, req *orderdto.CouponRedemptionListReq) (res *orderdto.CouponRedemptionListResp, err error)
}

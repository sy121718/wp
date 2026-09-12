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
	// RefundOrder 退款：改状态 + 记流水号。
	//
	// **不归还库存** —— 退款是钱的事，退货入库是货的事，两者可以不同步
	// （比如只退运费、或有质量问题直接退款不退货）。合并成一步会让「只退款」
	// 这种正常诉求没法表达。
	RefundOrder(ctx context.Context, req *orderdto.RefundOrderReq) (err error)
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

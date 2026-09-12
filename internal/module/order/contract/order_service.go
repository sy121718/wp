// Package ordercontract 订单模块对外契约（BIZ-1 销售侧）。
package ordercontract

import (
	"context"

	orderdto "go_wp/internal/module/order/dto"
	inventorydto "go_wp/internal/module/product/inventory/dto"
)

// OrderService 订单模块对外能力。
type OrderService interface {
	// CreateOrder 建单：读商品事实落快照 → 扣库存 → 写订单（落在同一事务里）。
	CreateOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error)
	// GetOrder 订单详情（头 + 订单项 + 状态流转链）。
	GetOrder(ctx context.Context, orderID uint64) (res *orderdto.OrderDetailResp, err error)
	// ListOrders 订单列表 + 各状态计数。
	ListOrders(ctx context.Context, req *orderdto.ListOrderReq) (res *orderdto.OrderListResp, err error)
	// ChangeStatus 状态流转（哪条边合法由状态机判定）。
	ChangeStatus(ctx context.Context, req *orderdto.ChangeStatusReq) (err error)
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

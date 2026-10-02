// Package orderstock 把库存用例适配成订单侧的库存契约（ordercontract.StockOperator）。
//
// 为什么需要这一层：订单契约的入参是**订单自己的语义类型**
// （StockDeduction / StockAdjustment —— 「出哪几个 SKU、各多少件、什么原因」），
// 库存用例的入参是库存的 dto（带 skuCode 冗余列、expandBom 开关、direction 字符串
// 这些绑定层字段）。两条路都会留下问题：
//
//	· 让订单契约直接引用库存 dto —— 库存改一次请求字段，订单跟着编译错（审计 CQ-004）；
//	· 让库存用例改签名去吃订单契约类型 —— 库存自己的 HTTP 接口会被迫跟着改。
//
// 适配层是唯一同时满足两者的位置，而且依赖方向是对的：**实现方依赖调用方契约**
// （本包 → ordercontract），调用方（order）不认识库存的任何包。
//
// 映射口径与适配前逐字一致（含原因码与来源引用），行为不变：
// 出库方向恒为扣减，变动方向恒为 in —— 订单域没有别的方向。
package orderstock

import (
	"context"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryservice "go_wp/internal/module/inventory/service"
	ordercontract "go_wp/internal/module/order/contract"
)

// Operator 订单侧库存契约的库存实现。
type Operator struct {
	svc *inventoryservice.Service
}

// 编译期断言：适配器必须满足订单侧的库存契约。
var _ ordercontract.StockOperator = (*Operator)(nil)

// New 构造适配器。返回具体类型而不是接口：装配方拿到它直接传给订单模块，
// 不需要类型断言；也刻意不做「svc 为空返回 nil」—— 那会在赋给接口时变成
// **typed nil**（接口非空、调用才炸），不如让装配方在外层显式守卫。
func New(svc *inventoryservice.Service) *Operator {
	return &Operator{svc: svc}
}

// DeductStock 建单出库（原因码与来源引用由订单侧给，仓库按 SKU 归属仓解析）。
func (o *Operator) DeductStock(ctx context.Context, in *ordercontract.StockDeduction) (err error) {
	if in == nil {
		return nil
	}
	_, err = o.svc.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID:  in.ProjectID,
		ReasonCode: in.ReasonCode,
		SourceType: in.SourceType,
		SourceRef:  in.SourceRef,
		Remark:     in.Remark,
		Lines:      toStockLines(in.Lines),
	})
	return err
}

// ChangeStock 把货加回库存（取消订单归还 / 退货入库）。
//
// Direction 恒为 in：契约类型没有方向字段，方向是这条能力的语义本身。
//
// 走库存的 **RestockStock**（自动归还策略）而不是 ChangeStock：归还的是订单出库时
// **扣掉过**的数量，而无限（不跟踪）的行出库时根本没扣 —— 用普通入库语义会把那些行
// 切成跟踪，把「无限商品」变成「库存 N」，第 N+1 件直接被判库存不足（BIZ-03）。
// 本方法只服务订单归还，管理员手工入库 / 采购收货走的是另一条路（HTTP → ChangeStock）。
func (o *Operator) ChangeStock(ctx context.Context, in *ordercontract.StockAdjustment) (err error) {
	if in == nil {
		return nil
	}
	_, err = o.svc.RestockStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID:  in.ProjectID,
		Direction:  "in",
		ReasonCode: in.ReasonCode,
		SourceType: in.SourceType,
		SourceRef:  in.SourceRef,
		Remark:     in.Remark,
		Lines:      toStockLines(in.Lines),
	})
	return err
}

// DeductStockTx 建单出库的**事务透传版**：扣减落在订单自己的事务里。
//
// 适配层不做任何额外处理 —— 事务句柄原样交给库存服务，库存侧自己不会再开事务。
func (o *Operator) DeductStockTx(ctx context.Context, tx *gorm.DB, in *ordercontract.StockDeduction) (err error) {
	if in == nil {
		return nil
	}
	return o.svc.DeductStockTx(ctx, tx, &inventorydto.DeductStockReq{
		ProjectID:  in.ProjectID,
		ReasonCode: in.ReasonCode,
		SourceType: in.SourceType,
		SourceRef:  in.SourceRef,
		Remark:     in.Remark,
		Lines:      toStockLines(in.Lines),
	})
}

// ChangeStockTx 把货加回库存的**事务透传版**（取消归还 / 退货入库），Direction 恒为 in。
//
// 与 ChangeStock 同源：走 RestockStockTx（自动归还策略），对不跟踪的行跳过 ——
// 这是订单侧唯一实际在用的归还入口（order_status 取消归还、return_review 退货入库）。
func (o *Operator) ChangeStockTx(ctx context.Context, tx *gorm.DB, in *ordercontract.StockAdjustment) (err error) {
	if in == nil {
		return nil
	}
	return o.svc.RestockStockTx(ctx, tx, &inventorydto.ChangeStockReq{
		ProjectID:  in.ProjectID,
		Direction:  "in",
		ReasonCode: in.ReasonCode,
		SourceType: in.SourceType,
		SourceRef:  in.SourceRef,
		Remark:     in.Remark,
		Lines:      toStockLines(in.Lines),
	})
}

// toStockLines 契约行 → 库存 dto 行。
func toStockLines(lines []ordercontract.StockLine) []inventorydto.StockChangeLineReq {
	if len(lines) == 0 {
		return nil
	}
	out := make([]inventorydto.StockChangeLineReq, 0, len(lines))
	for _, l := range lines {
		out = append(out, inventorydto.StockChangeLineReq{
			WarehouseID: l.WarehouseID,
			ProductID:   l.ProductID,
			VariantID:   l.VariantID,
			SKUCode:     l.SKUCode,
			Quantity:    l.Quantity,
		})
	}
	return out
}

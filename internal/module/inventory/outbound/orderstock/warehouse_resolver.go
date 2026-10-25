package orderstock

// warehouse_resolver.go — 「这一行从哪个仓出」的库存侧实现（迁移 628，S2）。
//
// 订单侧持有 ordercontract.WarehouseResolver 这个窄端口，实现放在库存侧
// （与 Operator / WarehouseSource 同一手法：实现方依赖调用方契约）。
//
// 为什么不新写一份解析逻辑：库存域的 ResolveWarehouse 已经是「显式仓 → 默认仓」的
// 唯一判据，抄一份到订单侧就是两处会各自漂移的真源 —— 而两条路径（预占 / 发货）
// 一旦解析到不同的仓，账面就对不上了。这里只做形状转换。

import (
	"context"

	inventorycontract "go_wp/internal/module/inventory/contract"
	ordercontract "go_wp/internal/module/order/contract"
)

// WarehouseResolver 订单侧发货仓解析的库存实现。
//
// 依赖的是 ProductStockLookup（只含 ResolveWarehouse 等只读能力）而不是整个
// InventoryService：宽端口会让「这一层只要解析一个仓」的事实看不出来，
// 也给了它顺手调别的方法的余地。
type WarehouseResolver struct {
	svc inventorycontract.ProductStockLookup
}

// 编译期断言：适配器必须满足订单侧的发货仓解析端口。
var _ ordercontract.WarehouseResolver = (*WarehouseResolver)(nil)

// NewWarehouseResolver 构造（svc 为库存契约：只用 ResolveWarehouse 一条能力）。
//
// 与 New 一样返回具体类型而不是接口：装配方直接传给订单模块；
// 也刻意不做「svc 为空返回 nil」—— 那会在赋给接口时变成 typed nil（接口非空、调用才炸）。
func NewWarehouseResolver(svc inventorycontract.ProductStockLookup) *WarehouseResolver {
	return &WarehouseResolver{svc: svc}
}

// ResolveShipWarehouse 解析发货仓：requested 非空即用它（库存侧校验工程与 active），
// 为空回落到默认仓；工程里一个仓都没有时返回空串 + nil。
//
// 「没有仓」与「解析失败」必须分开：前者是合法的单站形态（虚拟商品 / 纯服务工程），
// 下单不该被拦住；后者（指定的仓不存在 / 跨工程 / 已停用）必须当场报错。
func (r *WarehouseResolver) ResolveShipWarehouse(ctx context.Context, projectID, requested string) (string, error) {
	ref, err := r.svc.ResolveWarehouse(ctx, projectID, requested)
	if err != nil {
		// 库存侧在没有默认仓时报 ErrWarehouseDefaultMissing —— 那正是「这个工程没有可用仓」，
		// 对订单侧来说不是错误（本地单站允许无仓卖货）。其余错误原样上抛。
		if isNoWarehouse(err) {
			return "", nil
		}
		return "", err
	}
	if ref == nil {
		return "", nil
	}
	return ref.ID, nil
}

// isNoWarehouse 判「这个工程没有可用仓」（而不是「指定的仓不可用」）。
func isNoWarehouse(err error) bool {
	return err != nil && isDefaultMissing(err)
}

// isDefaultMissing 判错误是否为「缺默认仓」。
//
// 用字符串比对（inventoryenums.ErrWarehouseDefaultMissing 的常量值）——
// 与 order 侧既有的 ErrStockInsufficient 比对同一手法：跨模块只依赖契约，
// 不 import 对方的 enums（module_boundary_test.go 的反向依赖判据）。
func isDefaultMissing(err error) bool {
	return err != nil && err.Error() == "ErrWarehouseDefaultMissing"
}

// variant_cost.go — 变体成本价端口（issue #18）。
//
// 依赖方向仍是 inventory → product：本模块只**定义端口**（不认识库存模块的实现），
// 由 inventory 模块在「采购入库 / 生产入库」时调用，把入库单价写回 product_variants.cost_price。
//
// 与 VariantStockCachePort 同属「product 实现、inventory 调用」那一半，区别在语义：
//
//	VariantStockCachePort —— 库存真源变动后同步**展示缓存**（stock_total），失败可对账兜底；
//	VariantCostPort       —— 入库登记时写回**业务列**（cost_price，采购/生产成本口径），
//	                         它参与定价工具（成本乘倍数等规则），是真实业务数据。
//
// 两条不可动摇的约定：
//
//  1. 只更新 cost_price 一列 —— 售价（price）、划线价（compare_price）、
//     库存缓存（stock_total）等一律不碰；
//  2. 调用发生在库存变动**提交之后**（跨模块写不进同一个事务），失败由调用方
//     记在入库单行上（cost_updated / cost_error），不回滚已经落地的真源库存。
package productcontract

import "context"

// VariantCostPort 变体成本价写回端口（由 product 模块实现，inventory 模块调用）。
type VariantCostPort interface {
	// UpdateVariantCost 把某个变体的成本价写成 cost 并盖上更新时间。
	// 变体不存在时返回错误（调用方按回写失败记账，不影响已经落地的库存变动）。
	//
	// operatorID 是这次入库登记的操作人（issue #19 起成本价回写要进主数据变更记录，
	// 记录里的「谁改的」取自这里）；缺失时传空串，留痕字段允许为空。
	//
	// projectID 是工程隔离（DB-009）的作用域来源：回写要先按变体反查所属商品，
	// 而 products 在迁移 215 名单里 —— 没有工程作用域时这次读会静默返回 0 行，
	// 表现为「成本价回写失败」而没有任何错误日志。调用方（inventory 的入库登记）
	// 手里有入库单行的 project_id，直接传下来即可。
	UpdateVariantCost(ctx context.Context, projectID, variantID string, cost float64, operatorID string) (err error)
}

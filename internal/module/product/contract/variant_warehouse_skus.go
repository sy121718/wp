package productcontract

// variant_warehouse_skus.go — 「变体引用了哪些仓库货行」的跨模块读口（迁移 637）。
//
// 关联的真源在 product_variants.warehouse_skus（JSON 数组，元素 {stockId,warehouseId,
// warehouseCode,skuCode}）。库存 / 订单域要按变体找货、或按货找变体时，只能经这个口，
// **不能去读 product_variants 表**（模块边界：表只被一个模块读写）。
//
// 为什么这两个方法是「读口」而不是「同模块直调」：库存域要按变体定位货行（预占 / 扣减 /
// 汇总），订单域要按变体确认「这批货能不能卖」—— 两处都在别的模块里，且都是**主链路**。
// 所以本仓不用内嵌端口那种「共用形状重导出」的手法，而是直接给出窄接口：
// 入参出参都是基本类型，调用方不需要认识商品的任何 dto。

import "context"

// VariantWarehouseStockPort 变体 ↔ 仓库货行的双向查询。
//
// 两个方向都要，缺一个就有活干不了：
//   - 正向（变体 → 货行）服务库存寻址：预占 / 扣减 / 汇总都从「哪个变体、要多少件」开始；
//   - 反向（货行 → 变体）服务守卫：删货行 / 删仓 / 删变体之前要问「还有谁引用它」。
type VariantWarehouseStockPort interface {
	// WarehouseStockIDsByVariant 按变体列出它引用的库存行 id（跨仓，顺序按 JSON 里的记录顺序）。
	//
	// 未引用任何货行的变体不出现在返回值里（调用方按空清单兜底）—— 空清单与「变体不存在」
	// 是两件事，但调用方对两者的处理一样（都拿不到可扣的货），所以不额外区分。
	WarehouseStockIDsByVariant(ctx context.Context, projectID string, variantIDs []string) (ids map[string][]string, err error)

	// VariantIDsByWarehouseStock 反查：这条库存行被哪些变体引用着。
	//
	// 返回空清单 = 没有变体引用它（一条「仓库里有货、还没有商品卖它」的存货）。
	// 守卫据此区分「可以删」与「有人用着」。
	VariantIDsByWarehouseStock(ctx context.Context, projectID, stockID string) (variantIDs []string, err error)

	// VariantIDsByWarehouseStocks 批量反查（台账页一屏几十行，逐行查会变成 N 次往返）。
	//
	// 返回的 map 只包含**至少有一个变体引用**的 stockId；没被引用的行不出现在 map 里。
	VariantIDsByWarehouseStocks(ctx context.Context, projectID string, stockIDs []string) (byStock map[string][]string, err error)
}

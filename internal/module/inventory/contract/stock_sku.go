// stock_sku.go — 「变体 → 仓库侧裸码」的只读映射（后台采购下拉用）。
//
// 为什么要单列一个口：采购行的 sku_code 必须按**仓库侧裸码**落库（仓库里的 SKU 不带仓码前缀，
// 见 docs/14 §1.1 与迁移 262），而裸码的真源是 inventory_stocks.sku_code。
// 原先的写法是「每个商品查一次库存行」，N 个商品就是 N 次往返；
// 本口把整工程的映射一次拿完。
//
// 只读：没有任何写方法。
package inventorycontract

import "context"

// StockSKUReader 工程内「变体 id → 仓库侧裸码」的映射。
//
// 同一个变体在多个仓可能有不同的裸码：实现方按固定规则取一条（当前是仓库 sort 最靠前的
// 那条非空值），调用方不要再自行挑 —— 挑法两处各写一份，入库时就会按另一个仓的编码落库，
// 而两边都不报错。
type StockSKUReader interface {
	BareSKUsByVariant(ctx context.Context, projectID string) (map[string]string, error)
}

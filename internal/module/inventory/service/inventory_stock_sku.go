// inventory_stock_sku.go — 入库入口对 SKU 编码的**归一 + 校验**（仓库侧裸码口径的唯一落点）。
//
// 口径（docs/14 §1.1 与迁移 262，2026-09-19 冻结）：
//
//	· **仓库里的 SKU 永远是裸码**（DRAWERSMOKE_001），不带仓码前缀；
//	  仓码前缀只出现在**商品侧**（SZ_DRAWERSMOKE_001，标注归属 / 认领仓）。
//
// 为什么入库侧必须自己剥一次前缀，而不是继续指望「商品侧负责剥前缀」：
//
//	商品侧那条路径（product_crud / product_variant → EnsureVariantStock）剥的是
//	**商品自己那条 SKU**，它天然带着自己的认领仓前缀，两者恒对齐。入库不是那条路径：
//
//	  · 页面表单是操作者从候选里手选的编码；
//	  · 接口调用方（外部系统 / 脚本 / 运维）给的通常是商品侧的带前缀编码 —— docs/14
//	    只规定了「商品侧建库存行时剥前缀」，而入库建库存行根本不经商品侧；
//	  · 结果是**新建**的库存行可能落成空串或带前缀的编码，直接把不变量捅破：
//	    空串还会在 UNIQUE (warehouse_id, sku_code)（迁移 244）上撞成一句没有上下文的 23505。
//
// 所以本文件是入库入口的唯一规则，**空串一律拒绝**（绝不再用空串建库存行）。
// 归一实现与商品侧的 productservice.stripWarehousePrefix 逐字同口径（幂等 / 大小写不敏感 /
// 只剥一次），也与迁移 262 的存量清理谓词一致 —— 三处必须同时改，
// public/test/inventory/feature 里有一条把两边钉在一起的等价性断言。
//
// 为什么在同模块里再写一份而不直接调商品侧那个函数：本模块对商品模块的依赖**只允许经
// product 契约**（AGENTS.md 的表隔离约定），而 stripWarehousePrefix / StripWarehousePrefix
// 都不在契约上（它是商品的 service 层导出，给商品自己的 inbound 用的）。为这一处归一去
// 扩商品契约，会让「仓码前缀怎么剥」这条库存域规则挂到商品模块的对外接口上。
package inventoryservice

import (
	"errors"
	"strings"

	inventoryenums "go_wp/internal/module/inventory/enums"
)

// skuSeparator 仓码与前缀后编码之间的分隔符（与商品侧同一个字面量）。
const skuSeparator = "_"

// stripWarehousePrefix 剥掉仓码前缀 —— 商品侧 attachWarehousePrefix 的逆操作。
//
// 三条性质（与商品侧逐条对称）：
//
//	· **幂等** —— 本来不带前缀时原样返回，剥两次与剥一次相同；
//	· **大小写不敏感** —— 前缀按大写比对（仓短码在工程内已归一为大写，历史数据不一定）；
//	· **只剥一次** —— SZ_SZ_X 剥成 SZ_X（不会一路剥到 X）。
//
// 仓码为空（未选仓 / 端口未注入）时原样返回：没有前缀就无所谓剥离。
// 返回值可能为空串（调用方给的就是「SZ_」这种只有前缀的编码）—— 空值判定留给
// normalizeStockSKU，阈值口径不在本函数里再散一份。
func stripWarehousePrefix(code, warehouseCode string) string {
	trimmed := strings.TrimSpace(code)
	prefix := strings.ToUpper(strings.TrimSpace(warehouseCode))
	if prefix == "" {
		return trimmed
	}
	if strings.HasPrefix(strings.ToUpper(trimmed), prefix+skuSeparator) {
		return trimmed[len(prefix)+len(skuSeparator):]
	}
	return trimmed
}

// normalizeStockSKU 把调用方给的 SKU 编码归一成**仓库侧裸码**，空串一律拒绝。
//
// 选这条口径（而不是「拒绝带前缀」）的理由：
//
//	· 入库是本模块接收 SKU 编码的**唯一入口**，编码来源不可控（页面手选 / 外部系统 /
//	  运维脚本），而带前缀是它们的常态 —— 一律拒绝等于把「调用方没按我们的内部表示传参」
//	  判成业务错误，运营拿到的会是「编码不合法」而不是货收进来了；
//	· 幂等剥前缀是纯粹的口径**归一**（不改变编码语义），与商品侧建行的做法完全一致：
//	  同一条编码在两边得到同一结果，不会出现「同一个 SKU 在商品侧是裸码、在库存侧带前缀」；
//	· 归一之后仍为空（给了空串 / 只给了「SZ_」）→ **明确拒绝**，不再用空串建库存行。
func normalizeStockSKU(code, warehouseCode string) (bare string, err error) {
	bare = stripWarehousePrefix(code, warehouseCode)
	if bare == "" {
		return "", errors.New(inventoryenums.ErrStockSKURequired)
	}
	return bare, nil
}

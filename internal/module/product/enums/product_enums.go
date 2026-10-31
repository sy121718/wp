// Package productenums 声明商品领域枚举与响应事实。
package productenums

// 商品列表「库存」列的三态（docs/14 §1.4，2026-09-19 口径）。
//
// 一个状态只用一种表达方式：「无限」不写进数量列，「还没入库」也不写成一行 0。
// 三态是**互斥**的，混合状态（有的仓无限、有的仓跟踪）按「无限」显示 ——
// 求和等于把无限当 0，那是最糟的一种错（页面显示有货、其实卖不完）。
const (
	// StockStateInfinite 任一仓 track_quantity = false（不跟踪 = 无限）→ 显示 ∞。
	StockStateInfinite = "infinite"
	// StockStateTracked 全部仓都跟踪 → 显示各仓数量之和（0 就显示 0）。
	StockStateTracked = "tracked"
	// StockStateNone 该商品在任何仓都没有库存行 → 显示「未入库」。
	StockStateNone = "none"
)

// 商品状态。
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusArchived  = "archived"
)

// 标签类型（issue #11）：manual 手工挂载 / rule 按内置规则自动维护归属。
const (
	TagKindManual = "manual"
	TagKindRule   = "rule"
)

// 内置规则类型（issue #11）：只接受这几个类型 + 各自的白名单参数，
// 不接受自由表达式（SQL / 脚本 / 表达式串一律拒绝）。
const (
	// TagRuleNewArrival 新品：上架 N 天内（参数 days）。
	TagRuleNewArrival = "new_arrival"
	// TagRulePriceRange 价格区间：存在启用变体价格落在区间内（参数 minPrice / maxPrice，至少一个）。
	TagRulePriceRange = "price_range"
	// TagRuleOnSale 促销：存在启用变体有划线价（对比价高于售价）；无参数。
	TagRuleOnSale = "on_sale"
)

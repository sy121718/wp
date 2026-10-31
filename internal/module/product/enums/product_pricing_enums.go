package productenums

// 定价子域的枚举：规则类型、尾数处理、作用范围、逐变体处理结果与跳过原因。
//
// 这些取值只服务于「调价工具」（issue #13），与商品状态 / 标签类型不是同一组职责，
// 按 PRD-021 的主枚举规范拆到独立文件；取值本身一个字未改 —— 它们会进 JSON、
// 进留痕，改取值是数据迁移，不是整理。

// 定价规则类型（issue #13）：只接受这四个内置类型 + 各自的白名单参数，
// 不接受自由表达式。算出的售价**落库**（product_variants.price），
// 构建期读的是落库后的确定值 —— 定价工具不参与构建管线。
const (
	// PricingRuleCostMultiple 成本乘倍数：售价 = 成本 × multiplier。
	PricingRuleCostMultiple = "cost_multiple"
	// PricingRuleCostMarkup 成本加价：售价 = 成本 + amount。
	PricingRuleCostMarkup = "cost_markup"
	// PricingRuleTargetMargin 目标毛利率：售价 = 成本 ÷ (1 - margin)（margin 为 0~1 的目标毛利率）。
	PricingRuleTargetMargin = "target_margin"
	// PricingRuleFixedPrice 统一售价：售价 = amount（不看成本）。
	PricingRuleFixedPrice = "fixed_price"
)

// 尾数处理（issue #13）：一律**向上**取（只抬不降），保证按规则算出的价格不会被尾数处理压低。
const (
	// PricingRoundingNone 不舍入：四舍五入到分。
	PricingRoundingNone = "none"
	// PricingRoundingInteger 向上取整到元。
	PricingRoundingInteger = "integer"
	// PricingRoundingEnd9 尾数 9：向上取到角位为 9（12.34 → 12.90）。
	PricingRoundingEnd9 = "end_9"
	// PricingRoundingEnd99 尾数 99：向上取到分为 99（12.34 → 12.99）。
	PricingRoundingEnd99 = "end_99"
)

// 定价工具的作用范围（issue #13）。
const (
	// PricingScopeSKU 单个 SKU（一个变体）。
	PricingScopeSKU = "sku"
	// PricingScopeProduct 单个商品的全部变体。
	PricingScopeProduct = "product"
	// PricingScopeFilter 筛选出的商品集（其全部变体）。
	PricingScopeFilter = "filter"
)

// 逐变体试算行的处理结果（预览与留痕共用）。
const (
	// PricingLineChanged 价格发生变化。
	PricingLineChanged = "changed"
	// PricingLineUnchanged 试算结果与当前价格相同（不写库、不留痕）。
	PricingLineUnchanged = "unchanged"
	// PricingLineSkipped 该变体本轮无法计算（原因见 Reason）。
	PricingLineSkipped = "skipped"
)

// 逐变体跳过原因（issue #13；带成本类规则在缺成本价时跳过单个变体，
// 其余变体照常处理 —— 一个没填成本的 SKU 不该让整批调价失败）。
const (
	// PricingSkipCostMissing 变体没有成本价（NULL），按成本类规则无法计算。
	PricingSkipCostMissing = "PricingSkipCostMissing"
	// PricingSkipOutOfRange 计算结果为负或超过可存储上限（numeric(12,2)）。
	PricingSkipOutOfRange = "PricingSkipOutOfRange"
)

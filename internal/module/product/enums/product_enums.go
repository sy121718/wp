// Package productenums product 模块响应消息。
package productenums

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess = "MsgCreateSuccess"
	MsgUpdateSuccess = "MsgUpdateSuccess"
	MsgDeleteSuccess = "MsgDeleteSuccess"
	MsgListSuccess   = "MsgListSuccess"
	MsgDetailSuccess = "MsgDetailSuccess"
	// MsgVariantGenerateSuccess 变体组合生成成功（issue #8）。
	MsgVariantGenerateSuccess = "MsgVariantGenerateSuccess"

	ErrInvalidParam = "ErrInvalidParam" // 参数错误
	ErrInvalidType  = "ErrInvalidType"  // 实体类型不合法（构建期字段解析）
	ErrInvalidField = "ErrInvalidField" // 字段不在商品字段白名单内
	ErrNotFound     = "ErrNotFound"     // 商品或变体不存在
	ErrSlugTaken    = "ErrSlugTaken"    // 同工程下 slug 已占用
	ErrSkuTaken     = "ErrSkuTaken"     // 同商品下 SKU 编码已占用
	ErrNameRequired = "ErrNameRequired" // 商品名称必填

	// —— 属性组（issue #7）——
	ErrAttrNameRequired      = "ErrAttrNameRequired"      // 属性组名称必填
	ErrAttrKeyRequired       = "ErrAttrKeyRequired"       // 属性组标识不能改为空
	ErrAttrKeyTaken          = "ErrAttrKeyTaken"          // 同工程下属性组标识已占用
	ErrAttrNotFound          = "ErrAttrNotFound"          // 属性组不存在
	ErrAttrProjectMismatch   = "ErrAttrProjectMismatch"   // 属性组不属于该商品所在工程
	ErrAttrInUse             = "ErrAttrInUse"             // 属性组已被商品引用，不能删除
	ErrAttrValueLabelMissing = "ErrAttrValueLabelMissing" // 属性值名称必填

	// —— 分类（issue #10）——
	ErrCategoryNameRequired    = "ErrCategoryNameRequired"    // 分类名称必填
	ErrCategoryNotFound        = "ErrCategoryNotFound"        // 分类不存在
	ErrCategorySlugTaken       = "ErrCategorySlugTaken"       // 同工程下分类 slug 已占用
	ErrCategoryParentMismatch  = "ErrCategoryParentMismatch"  // 父分类不属于该分类所在工程
	ErrCategoryCycle           = "ErrCategoryCycle"           // 不能把分类挂到自身或自己的后代下
	ErrCategoryHasChildren     = "ErrCategoryHasChildren"     // 分类仍有子级，不能删除
	ErrCategoryInUse           = "ErrCategoryInUse"           // 分类已被商品引用，不能删除
	ErrCategoryProjectMismatch = "ErrCategoryProjectMismatch" // 分类不属于该商品所在工程

	// —— 品牌（issue #10）——
	ErrBrandNameRequired    = "ErrBrandNameRequired"    // 品牌名称必填
	ErrBrandNotFound        = "ErrBrandNotFound"        // 品牌不存在
	ErrBrandSlugTaken       = "ErrBrandSlugTaken"       // 同工程下品牌 slug 已占用
	ErrBrandInUse           = "ErrBrandInUse"           // 品牌已被商品引用，不能删除
	ErrBrandProjectMismatch = "ErrBrandProjectMismatch" // 品牌不属于该商品所在工程

	// —— 标签（issue #11）——
	ErrTagNameRequired      = "ErrTagNameRequired"      // 标签名称必填
	ErrTagNotFound          = "ErrTagNotFound"          // 标签不存在
	ErrTagSlugTaken         = "ErrTagSlugTaken"         // 同工程下标签 slug 已占用
	ErrTagProjectMismatch   = "ErrTagProjectMismatch"   // 标签不属于该商品所在工程
	ErrTagKindInvalid       = "ErrTagKindInvalid"       // 标签类型不是 manual / rule
	ErrTagRuleNotAllowed    = "ErrTagRuleNotAllowed"    // 手工标签不能带自动规则
	ErrTagRuleTypeInvalid   = "ErrTagRuleTypeInvalid"   // 规则类型不是内置类型（不接受自由表达式）
	ErrTagRuleParamsInvalid = "ErrTagRuleParamsInvalid" // 规则参数不合法（键/类型/取值范围）
	ErrTagNotManual         = "ErrTagNotManual"         // 自动标签的归属由重算维护，不能手工挂载

	// —— 集合源（issue #9）——
	ErrCollectionSourceInvalid = "ErrCollectionSourceInvalid" // 集合源标识不合法（不是本模块实现的源）
	ErrCollectionFilterInvalid = "ErrCollectionFilterInvalid" // 过滤维度不在集合源白名单内

	// —— 变体组合生成（issue #8）——
	// 上限类错误的详细数值由 service 拼进消息（如「：7 个组合超过上限 200」），
	// 这里只保留稳定的错误标识，避免同一上限在多处各写一份数字。
	ErrVariationNoDimension      = "ErrVariationNoDimension"      // 没有可参与变体的属性值
	ErrVariationDimensionLimit   = "ErrVariationDimensionLimit"   // 参与变体的属性维度超过上限
	ErrVariationCountLimit       = "ErrVariationCountLimit"       // 变体组合数超过上限
	ErrVariationAttributeInvalid = "ErrVariationAttributeInvalid" // 勾选的属性组未参与该商品的变体
	ErrVariationValueInvalid     = "ErrVariationValueInvalid"     // 勾选的属性值不属于该属性组或已停用
	ErrVariationSelectionEmpty   = "ErrVariationSelectionEmpty"   // 未勾选任何属性值
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

// 响应消息与业务错误（定价工具，issue #13）。
const (
	// MsgPricingPreviewSuccess 试算完成（未落库）。
	MsgPricingPreviewSuccess = "MsgPricingPreviewSuccess"
	// MsgPricingApplySuccess 调价已应用（落库 + 留痕）。
	MsgPricingApplySuccess = "MsgPricingApplySuccess"

	// ErrPricingRuleTypeInvalid 定价规则类型不是内置类型。
	ErrPricingRuleTypeInvalid = "ErrPricingRuleTypeInvalid"
	// ErrPricingRuleParamsInvalid 定价规则参数不合法（未知键 / 类型不符 / 取值越界）。
	ErrPricingRuleParamsInvalid = "ErrPricingRuleParamsInvalid"
	// ErrPricingRoundingInvalid 尾数处理方式不是内置类型。
	ErrPricingRoundingInvalid = "ErrPricingRoundingInvalid"
	// ErrPricingScopeInvalid 作用范围不是内置类型。
	ErrPricingScopeInvalid = "ErrPricingScopeInvalid"
	// ErrPricingTargetRequired 该作用范围缺少目标（单个 SKU / 单个商品必须给目标 id）。
	ErrPricingTargetRequired = "ErrPricingTargetRequired"
	// ErrPricingTargetNotFound 定价目标（SKU 或商品）不存在。
	ErrPricingTargetNotFound = "ErrPricingTargetNotFound"
	// ErrPricingFilterEmpty 筛选集没有命中任何商品（拒绝「空筛选全表改价」）。
	ErrPricingFilterEmpty = "ErrPricingFilterEmpty"
	// ErrPricingNothingChanged 没有任何变体的价格需要改动（不写库、不留痕）。
	ErrPricingNothingChanged = "ErrPricingNothingChanged"
	// ErrPricingAdjustmentNotFound 调价批次不存在。
	ErrPricingAdjustmentNotFound = "ErrPricingAdjustmentNotFound"
	// ErrPricingTargetTooMany 筛选集命中的商品数超过单批上限（拒绝「一次改太多」）。
	ErrPricingTargetTooMany = "ErrPricingTargetTooMany"
)

// 捆绑品（issue #20）。
//
// 配置期错误（保存时拒，运营填错了）与选择期错误（前台即时反馈，买家选错了）分开：
// 后台与前台因此都能给出具体原因，而不是一句笼统的「不合法」。
const (
	// MsgBundleSaveSuccess 捆绑配置已保存。
	MsgBundleSaveSuccess = "MsgBundleSaveSuccess"
	// MsgBundleValidateSuccess 整单校验通过。
	MsgBundleValidateSuccess = "MsgBundleValidateSuccess"

	// ErrBundleShapeInvalid 捆绑配置形状非法（既不是空值，也不是 JSON 对象）。
	ErrBundleShapeInvalid = "ErrBundleShapeInvalid"
	// ErrBundleNotConfigured 该商品没有配置捆绑选项（不是捆绑品）。
	ErrBundleNotConfigured = "ErrBundleNotConfigured"
	// ErrBundleMaxOptionsInvalid 选项数量上限非法（超出 1..20 护栏）。
	ErrBundleMaxOptionsInvalid = "ErrBundleMaxOptionsInvalid"
	// ErrBundleOptionsExceeded 配置的选项数超过上限。
	ErrBundleOptionsExceeded = "ErrBundleOptionsExceeded"

	// ErrBundleVariantRequired 选项缺少 SKU（配置期与选择期共用）。
	ErrBundleVariantRequired = "ErrBundleVariantRequired"
	// ErrBundleVariantDuplicated 同一个 SKU 在配置里或同一次选择里出现多次。
	ErrBundleVariantDuplicated = "ErrBundleVariantDuplicated"
	// ErrBundleVariantNotFound 选项引用的 SKU 不存在（已被删除）。
	ErrBundleVariantNotFound = "ErrBundleVariantNotFound"
	// ErrBundleVariantProjectMismatch 选项引用的 SKU 不属于该商品所在工程。
	ErrBundleVariantProjectMismatch = "ErrBundleVariantProjectMismatch"
	// ErrBundleSelfReference 选项引用了捆绑主体自己的 SKU（自引用）。
	ErrBundleSelfReference = "ErrBundleSelfReference"
	// ErrBundleVariantNotInConfig 选择里出现配置之外的 SKU。
	ErrBundleVariantNotInConfig = "ErrBundleVariantNotInConfig"

	// ErrBundleQtyInvalid 数量非法（负数 / 非整数 / 超过硬上限）。
	ErrBundleQtyInvalid = "ErrBundleQtyInvalid"
	// ErrBundleQtyRangeInvalid 单项数量区间自相矛盾（最小 > 默认、最大 < 最小 等）。
	ErrBundleQtyRangeInvalid = "ErrBundleQtyRangeInvalid"
	// ErrBundleTotalRangeInvalid 整单件数区间自相矛盾（最大 < 最小，或低于必选项最小量之和）。
	ErrBundleTotalRangeInvalid = "ErrBundleTotalRangeInvalid"
	// ErrBundleTotalUnreachable 整单下限高于所有选项能加到的上限（永远无法满足）。
	ErrBundleTotalUnreachable = "ErrBundleTotalUnreachable"

	// ErrBundleOptionRequired 漏填必选项。
	ErrBundleOptionRequired = "ErrBundleOptionRequired"
	// ErrBundleQtyBelowMin 单项数量低于该项最小数量。
	ErrBundleQtyBelowMin = "ErrBundleQtyBelowMin"
	// ErrBundleQtyAboveMax 单项数量超过该项最大数量。
	ErrBundleQtyAboveMax = "ErrBundleQtyAboveMax"
	// ErrBundleTotalBelowMin 整单总件数低于最小总件数。
	ErrBundleTotalBelowMin = "ErrBundleTotalBelowMin"
	// ErrBundleTotalAboveMax 整单总件数超过最大总件数。
	ErrBundleTotalAboveMax = "ErrBundleTotalAboveMax"

	// ErrBundleQtyAboveStock 数量超过库存可用量（只读库房真源得出的结论）。
	ErrBundleQtyAboveStock = "ErrBundleQtyAboveStock"
	// ErrBundleStockUnavailable 读不到库存可用量（inventory 端口未注入）：整单校验 fail-closed。
	ErrBundleStockUnavailable = "ErrBundleStockUnavailable"
)

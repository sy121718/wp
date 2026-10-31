package productenums

// 商品错误明细声明。service 交付键与具名参数，响应出口取词并填充；
// 缺词或占位符无效时使用声明处中文兜底。
const (
	// 定价规则参数。
	DetailPricingParamsNotObject   = "product.err.pricingParamsNotObject"
	DetailPricingParamRequired     = "product.err.pricingParamRequired"
	DetailPricingParamNotNumber    = "product.err.pricingParamNotNumber"
	DetailPricingParamMissingOrNaN = "product.err.pricingParamMissingOrNaN"
	DetailPricingParamRange        = "product.err.pricingParamRange"
	DetailPricingParamRangePlain   = "product.err.pricingParamRangePlain"
	DetailPricingUnknownKeys       = "product.err.pricingUnknownKeys"
	DetailPricingUnknownRuleType   = "product.err.pricingUnknownRuleType"
	DetailPricingUnknownRounding   = "product.err.pricingUnknownRounding"

	// 标签规则参数。
	DetailTagParamsNotObject = "product.err.tagParamsNotObject"
	DetailTagParamRequired   = "product.err.tagParamRequired"
	DetailTagParamNotInteger = "product.err.tagParamNotInteger"
	DetailTagParamNotNumber  = "product.err.tagParamNotNumber"
	DetailTagParamRange      = "product.err.tagParamRange"
	DetailTagParamRangePlain = "product.err.tagParamRangePlain"
	DetailTagUnknownKeys     = "product.err.tagUnknownKeys"
	DetailTagPriceNeedsOne   = "product.err.tagPriceNeedsOne"
	DetailTagPriceOrderWrong = "product.err.tagPriceOrderWrong"
	DetailTagUnknownRuleType = "product.err.tagUnknownRuleType"

	// —— 变体组合（service/product_variant.go）——
	DetailVariationCountExceed           = "product.err.variationCountExceed"
	DetailVariationDimensionExceed       = "product.err.variationDimensionExceed"
	DetailVariationAttributeUnreferenced = "product.err.variationAttributeUnreferenced"
	DetailVariationAttributeNotInProduct = "product.err.variationAttributeNotInProduct"
	DetailVariationValueNotEnabled       = "product.err.variationValueNotEnabled"

	// —— 批量改价范围（service/product_pricing.go）——
	DetailPricingFilterProjectRequired   = "product.err.pricingFilterProjectRequired"
	DetailPricingFilterConditionRequired = "product.err.pricingFilterConditionRequired"
	DetailPricingTargetTooMany           = "product.err.pricingTargetTooMany"

	// 相关商品引用。
	DetailRelatedSelfReference = "product.err.relatedSelfReference"
	DetailRelatedMissing       = "product.err.relatedMissing"
	DetailRelatedCrossProject  = "product.err.relatedCrossProject"

	// —— 删除守卫的跨工程引用明细（service/product_ref_guard.go）——
	DetailRefGuardBlocked = "product.err.refGuardBlocked"
)

// ErrDetailFallbacks 上面那组词条的中文兜底（i18n 未初始化 / 该 key 没有词条时用）。
//
// 占位符与词条一一对应（{field} / {n} / {max} / {attribute} / {value} / {ids} / {keys} …）：
// 读侧填不上某个占位符时判为坏词条、回落到这里的中文原文，而不是把 {field} 摆给运营看。
var ErrDetailFallbacks = map[string]string{
	DetailPricingParamsNotObject:   "参数必须是 JSON 对象",
	DetailPricingParamRequired:     "{field} 必填",
	DetailPricingParamNotNumber:    "{field} 必须是数字",
	DetailPricingParamMissingOrNaN: "{field} 缺失或不是数字",
	DetailPricingParamRange:        "{field} 必须在 {min}~{max} 之间，实际 {value}",
	DetailPricingParamRangePlain:   "{field} 必须在 {min}~{max} 之间",
	DetailPricingUnknownKeys:       "不支持的参数键：{keys}",
	DetailPricingUnknownRuleType:   "未知定价规则：{type}",
	DetailPricingUnknownRounding:   "未知尾数处理：{value}",

	DetailTagParamsNotObject: "参数必须是 JSON 对象",
	DetailTagParamRequired:   "{field} 必填",
	DetailTagParamNotInteger: "{field} 必须是整数",
	DetailTagParamNotNumber:  "{field} 必须是数字",
	DetailTagParamRange:      "{field} 必须在 {min}~{max} 之间，实际 {value}",
	DetailTagParamRangePlain: "{field} 必须在 {min}~{max} 之间",
	DetailTagUnknownKeys:     "不支持的参数键：{keys}",
	DetailTagPriceNeedsOne:   "minPrice 与 maxPrice 至少要给一个",
	DetailTagPriceOrderWrong: "minPrice 不能大于 maxPrice",
	DetailTagUnknownRuleType: "未知规则类型：{type}",

	DetailVariationCountExceed:           "{n} 个组合超过上限 {max}（请减少勾选的属性值或属性维度）",
	DetailVariationDimensionExceed:       "{n} 个维度超过上限 {max}（请减少参与变体的属性组）",
	DetailVariationAttributeUnreferenced: "属性组 {attribute} 未被该商品引用，或未标记为参与变体",
	DetailVariationAttributeNotInProduct: "属性组 {attribute} 未参与该商品的变体",
	DetailVariationValueNotEnabled:       "属性组 {attribute} 下没有启用中的属性值 {value}",

	DetailPricingFilterProjectRequired:   "筛选集范围必须指定工程",
	DetailPricingFilterConditionRequired: "筛选集至少要给一个筛选条件",
	DetailPricingTargetTooMany:           "{n} 个商品超过单批上限 {max}，请收紧筛选条件",

	DetailRelatedSelfReference: "不能指向自己（{n} 个：{ids}）",
	DetailRelatedMissing:       "不存在（{n} 个：{ids}）",
	DetailRelatedCrossProject:  "不属于本工程（{n} 个：{ids}；每条形如 商品id@所属工程id）",

	DetailRefGuardBlocked: "引用面 {columns}；命中 {n} 个商品、涉及 {projects} 个工程（{refs}）。请先在对应工程解绑后重试（守卫不自动清理）",
}

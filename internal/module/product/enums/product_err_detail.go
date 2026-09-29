package productenums

// product_err_detail.go — 商品域业务错误的**补充说明**词条（key + 具名参数 + 中文兜底）。
//
// 背景（系统性通道，见 pkg/i18n/errdetail.go）：service 判断被拒时习惯写成
//
//	fmt.Errorf("%s：%s", enums.ErrXxx, "属性组 a1 未参与该商品的变体")
//
// 读侧（productErrText）只按「key：」前缀认出前半截业务 key 去取词，**后半截中文原样
// 拼在译文后面** —— 英文界面上永远是中文，而它恰好是信息量最大的那一半。
//
// 本文件是那半句的真源：service 侧用 i18n.ErrorDetail(常量, name, value, …) 产出
// 「key + 参数」，读侧取词并填 {name} 占位符。中文兜底留在这里（i18n 未初始化 /
// 词条缺失时用），与 productErrFallbacks 同一形态。
const (
	// —— 定价规则参数（service/product_pricing_rule.go）——
	DetailPricingParamsNotObject   = "admin.product_pricing.detail.paramsNotObject"
	DetailPricingParamRequired     = "admin.product_pricing.detail.paramRequired"
	DetailPricingParamNotNumber    = "admin.product_pricing.detail.paramNotNumber"
	DetailPricingParamMissingOrNaN = "admin.product_pricing.detail.paramMissingOrNotNumber"
	DetailPricingParamRange        = "admin.product_pricing.detail.paramOutOfRange"
	DetailPricingParamRangePlain   = "admin.product_pricing.detail.paramOutOfRangePlain"
	DetailPricingUnknownKeys       = "admin.product_pricing.detail.unknownKeys"
	DetailPricingUnknownRuleType   = "admin.product_pricing.detail.unknownRuleType"
	DetailPricingUnknownRounding   = "admin.product_pricing.detail.unknownRounding"

	// —— 标签规则参数（service/product_tag_rule.go）——
	DetailTagParamsNotObject  = "admin.product_tags.detail.paramsNotObject"
	DetailTagParamRequired    = "admin.product_tags.detail.paramRequired"
	DetailTagParamNotInteger  = "admin.product_tags.detail.paramNotInteger"
	DetailTagParamNotNumber   = "admin.product_tags.detail.paramNotNumber"
	DetailTagParamRange       = "admin.product_tags.detail.paramOutOfRange"
	DetailTagParamRangePlain  = "admin.product_tags.detail.paramOutOfRangePlain"
	DetailTagUnknownKeys      = "admin.product_tags.detail.unknownKeys"
	DetailTagPriceNeedsOne    = "admin.product_tags.detail.priceNeedsOne"
	DetailTagPriceOrderWrong  = "admin.product_tags.detail.priceOrderWrong"
	DetailTagUnknownRuleType  = "admin.product_tags.detail.unknownRuleType"

	// —— 变体组合（service/product_variant_generate.go / product_bundle.go）——
	DetailVariationCountExceed          = "admin.products.detail.variationCountExceed"
	DetailVariationDimensionExceed      = "admin.products.detail.variationDimensionExceed"
	DetailVariationAttributeUnreferenced = "admin.products.detail.variationAttributeUnreferenced"
	DetailVariationAttributeNotInProduct = "admin.products.detail.variationAttributeNotInProduct"
	DetailVariationValueNotEnabled       = "admin.products.detail.variationValueNotEnabled"

	// —— 批量改价范围（service/product_pricing.go）——
	DetailPricingFilterProjectRequired   = "admin.product_pricing.detail.filterProjectRequired"
	DetailPricingFilterConditionRequired = "admin.product_pricing.detail.filterConditionRequired"
	DetailPricingTargetTooMany           = "admin.product_pricing.detail.targetTooMany"

	// —— 相关商品引用（service/product_crud.go）——
	DetailRelatedSelfReference = "admin.products.detail.relatedSelfReference"
	DetailRelatedMissing       = "admin.products.detail.relatedMissing"
	DetailRelatedCrossProject  = "admin.products.detail.relatedCrossProject"

	// —— 删除守卫的跨工程引用明细（service/product_ref_guard.go）——
	DetailRefGuardBlocked = "admin.products.detail.refGuardBlocked"
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

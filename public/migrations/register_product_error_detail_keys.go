package migrations

// 697b：本批与「产物 SEO 体检结果区词条」（697-seo-audit-i18n）同主编号，
// 因此按仓规补补丁位（同号 + 不同补丁位才合法，见 register_registry_test.go 的
// TestSeedVersionSlotsUnique 与 155b / 199b / 228b 的先例）。
// 排序影响：compareVersion 先比主编号、再比补丁位，"" < "b"（空串前缀更短先排），
// 所以 697 仍在 697b 之前 —— 两条 seed 互不依赖，顺序变化不影响结果。
func init() {
	registerSeed(Seed{
		Version:   "697b-product-error-detail-keys",
		TableName: "sys_i18n",
		ConditionSQL: `WITH keys(old_key, new_key) AS (VALUES
('admin.product_pricing.detail.paramsNotObject', 'product.err.pricingParamsNotObject'),
('admin.product_pricing.detail.paramRequired', 'product.err.pricingParamRequired'),
('admin.product_pricing.detail.paramNotNumber', 'product.err.pricingParamNotNumber'),
('admin.product_pricing.detail.paramMissingOrNotNumber', 'product.err.pricingParamMissingOrNaN'),
('admin.product_pricing.detail.paramOutOfRange', 'product.err.pricingParamRange'),
('admin.product_pricing.detail.paramOutOfRangePlain', 'product.err.pricingParamRangePlain'),
('admin.product_pricing.detail.unknownKeys', 'product.err.pricingUnknownKeys'),
('admin.product_pricing.detail.unknownRuleType', 'product.err.pricingUnknownRuleType'),
('admin.product_pricing.detail.unknownRounding', 'product.err.pricingUnknownRounding'),
('admin.product_tags.detail.paramsNotObject', 'product.err.tagParamsNotObject'),
('admin.product_tags.detail.paramRequired', 'product.err.tagParamRequired'),
('admin.product_tags.detail.paramNotInteger', 'product.err.tagParamNotInteger'),
('admin.product_tags.detail.paramNotNumber', 'product.err.tagParamNotNumber'),
('admin.product_tags.detail.paramOutOfRange', 'product.err.tagParamRange'),
('admin.product_tags.detail.paramOutOfRangePlain', 'product.err.tagParamRangePlain'),
('admin.product_tags.detail.unknownKeys', 'product.err.tagUnknownKeys'),
('admin.product_tags.detail.priceNeedsOne', 'product.err.tagPriceNeedsOne'),
('admin.product_tags.detail.priceOrderWrong', 'product.err.tagPriceOrderWrong'),
('admin.product_tags.detail.unknownRuleType', 'product.err.tagUnknownRuleType'),
('admin.products.detail.variationCountExceed', 'product.err.variationCountExceed'),
('admin.products.detail.variationDimensionExceed', 'product.err.variationDimensionExceed'),
('admin.products.detail.variationAttributeUnreferenced', 'product.err.variationAttributeUnreferenced'),
('admin.products.detail.variationAttributeNotInProduct', 'product.err.variationAttributeNotInProduct'),
('admin.products.detail.variationValueNotEnabled', 'product.err.variationValueNotEnabled'),
('admin.product_pricing.detail.filterProjectRequired', 'product.err.pricingFilterProjectRequired'),
('admin.product_pricing.detail.filterConditionRequired', 'product.err.pricingFilterConditionRequired'),
('admin.product_pricing.detail.targetTooMany', 'product.err.pricingTargetTooMany'),
('admin.products.detail.relatedSelfReference', 'product.err.relatedSelfReference'),
('admin.products.detail.relatedMissing', 'product.err.relatedMissing'),
('admin.products.detail.relatedCrossProject', 'product.err.relatedCrossProject'),
('admin.products.detail.refGuardBlocked', 'product.err.refGuardBlocked')
)
SELECT CASE WHEN
 NOT EXISTS (SELECT 1 FROM keys CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
 WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.new_key AND t.lang = langs.lang))
 AND NOT EXISTS (SELECT 1 FROM keys JOIN sys_i18n source ON source.item_key = keys.old_key
 WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.new_key AND t.lang = source.lang))
 THEN 1 ELSE 0 END`,
		SQL: mustSQL("697b_product_error_detail_keys.sql"),
	})
}

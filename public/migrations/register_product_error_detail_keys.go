package migrations

func init() {
	registerSeed(Seed{
		Version:   "697b-product-error-detail-keys",
		TableName: "sys_i18n",
		ConditionSQL: `WITH keys(old_key, new_key) AS (VALUES
('admin.product_pricing.detail.paramsNotObject', 'pPriceObjectReq'),
('admin.product_pricing.detail.paramRequired', 'pPriceArgReq'),
('admin.product_pricing.detail.paramNotNumber', 'pPriceArgNum'),
('admin.product_pricing.detail.paramMissingOrNotNumber', 'pPriceArgNaN'),
('admin.product_pricing.detail.paramOutOfRange', 'pPriceArgRange'),
('admin.product_pricing.detail.paramOutOfRangePlain', 'pPriceArgBounds'),
('admin.product_pricing.detail.unknownKeys', 'pPriceExtraKeys'),
('admin.product_pricing.detail.unknownRuleType', 'pPriceRuleName'),
('admin.product_pricing.detail.unknownRounding', 'pPriceRoundName'),
('admin.product_tags.detail.paramsNotObject', 'pTagObjectReq'),
('admin.product_tags.detail.paramRequired', 'pTagArgReq'),
('admin.product_tags.detail.paramNotInteger', 'pTagArgInt'),
('admin.product_tags.detail.paramNotNumber', 'pTagArgNum'),
('admin.product_tags.detail.paramOutOfRange', 'pTagArgRange'),
('admin.product_tags.detail.paramOutOfRangePlain', 'pTagArgBounds'),
('admin.product_tags.detail.unknownKeys', 'pTagExtraKeys'),
('admin.product_tags.detail.priceNeedsOne', 'pTagPriceReq'),
('admin.product_tags.detail.priceOrderWrong', 'pTagPriceOrder'),
('admin.product_tags.detail.unknownRuleType', 'pTagRuleName'),
('admin.products.detail.variationCountExceed', 'pVarCountExcess'),
('admin.products.detail.variationDimensionExceed', 'pVarDimExcess'),
('admin.products.detail.variationAttributeUnreferenced', 'pVarAttrUnused'),
('admin.products.detail.variationAttributeNotInProduct', 'pVarAttrOutside'),
('admin.products.detail.variationValueNotEnabled', 'pVarValueOff'),
('admin.product_pricing.detail.filterProjectRequired', 'pPriceFilterCtx'),
('admin.product_pricing.detail.filterConditionRequired', 'pPriceFilterReq'),
('admin.product_pricing.detail.targetTooMany', 'pPriceBatchSize'),
('admin.products.detail.relatedSelfReference', 'pRelatedSelf'),
('admin.products.detail.relatedMissing', 'pRelatedMissing'),
('admin.products.detail.relatedCrossProject', 'pRelatedScope'),
('admin.products.detail.refGuardBlocked', 'pRefGuardBlock')
)
SELECT CASE WHEN
 NOT EXISTS (SELECT 1 FROM keys CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
 WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.new_key AND t.lang = langs.lang))
 AND NOT EXISTS (SELECT 1 FROM keys JOIN sys_i18n source ON source.item_key = keys.old_key
 WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.new_key AND t.lang = source.lang))
 THEN 1 ELSE 0 END`,
		SQL: mustSQL("718p_current_697b_product_error_detail_keys.sql"),
	})
}

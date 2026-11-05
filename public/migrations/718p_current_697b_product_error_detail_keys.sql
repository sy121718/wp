-- 当前词条回补源；历史原文 697b_product_error_detail_keys.sql 保持归档，不再执行。
-- 复制既有全部语言及运营译文；目标已存在时保留目标。
WITH keys(old_key, new_key) AS (VALUES
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
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
SELECT keys.new_key, source.lang, source.item_value, source.http_code, 'product', source.remark, source.status, now(), now()
FROM keys JOIN sys_i18n source ON source.item_key = keys.old_key
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('pPriceFilterReq', 'product.err.pricingFilterConditionRequired'), ('pPriceFilterCtx', 'product.err.pricingFilterProjectRequired'), ('pPriceArgNaN', 'product.err.pricingParamMissingOrNaN'), ('pPriceArgNum', 'product.err.pricingParamNotNumber'), ('pPriceArgRange', 'product.err.pricingParamRange'), ('pPriceArgBounds', 'product.err.pricingParamRangePlain'), ('pPriceArgReq', 'product.err.pricingParamRequired'), ('pPriceObjectReq', 'product.err.pricingParamsNotObject'), ('pPriceBatchSize', 'product.err.pricingTargetTooMany'), ('pPriceExtraKeys', 'product.err.pricingUnknownKeys'), ('pPriceRoundName', 'product.err.pricingUnknownRounding'), ('pPriceRuleName', 'product.err.pricingUnknownRuleType'), ('pRefGuardBlock', 'product.err.refGuardBlocked'), ('pRelatedScope', 'product.err.relatedCrossProject'), ('pRelatedMissing', 'product.err.relatedMissing'), ('pRelatedSelf', 'product.err.relatedSelfReference'), ('pTagArgInt', 'product.err.tagParamNotInteger'), ('pTagArgNum', 'product.err.tagParamNotNumber'), ('pTagArgRange', 'product.err.tagParamRange'), ('pTagArgBounds', 'product.err.tagParamRangePlain'), ('pTagArgReq', 'product.err.tagParamRequired'), ('pTagObjectReq', 'product.err.tagParamsNotObject'), ('pTagPriceReq', 'product.err.tagPriceNeedsOne'), ('pTagPriceOrder', 'product.err.tagPriceOrderWrong'), ('pTagExtraKeys', 'product.err.tagUnknownKeys'), ('pTagRuleName', 'product.err.tagUnknownRuleType'), ('pVarAttrOutside', 'product.err.variationAttributeNotInProduct'), ('pVarAttrUnused', 'product.err.variationAttributeUnreferenced'), ('pVarCountExcess', 'product.err.variationCountExceed'), ('pVarDimExcess', 'product.err.variationDimensionExceed'), ('pVarValueOff', 'product.err.variationValueNotEnabled')) AS renamed(new_key, old_key)
    JOIN sys_i18n previous ON previous.item_key = renamed.old_key AND previous.lang = source.lang
    WHERE renamed.new_key = keys.new_key)
ON CONFLICT (item_key, lang) DO NOTHING;

-- 缺少真实来源时才补基础语言。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) 
SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
('pPriceObjectReq', 'zh-CN', '参数必须是 JSON 对象', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceObjectReq', 'en-US', 'Parameters must be a JSON object', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceArgReq', 'zh-CN', '{field} 必填', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceArgReq', 'en-US', '{field} is required', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceArgNum', 'zh-CN', '{field} 必须是数字', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceArgNum', 'en-US', '{field} must be a number', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceArgNaN', 'zh-CN', '{field} 缺失或不是数字', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceArgNaN', 'en-US', '{field} is missing or not a number', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceArgRange', 'zh-CN', '{field} 必须在 {min}~{max} 之间，实际 {value}', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceArgRange', 'en-US', '{field} must be between {min} and {max}, got {value}', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceArgBounds', 'zh-CN', '{field} 必须在 {min}~{max} 之间', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceArgBounds', 'en-US', '{field} must be between {min} and {max}', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceExtraKeys', 'zh-CN', '不支持的参数键：{keys}', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceExtraKeys', 'en-US', 'Unsupported parameter keys: {keys}', 200, 'product', 'pricing rule params error detail', 1, now(), now()),
('pPriceRuleName', 'zh-CN', '未知定价规则：{type}', 200, 'product', 'pricing rule type error detail', 1, now(), now()),
('pPriceRuleName', 'en-US', 'Unknown pricing rule: {type}', 200, 'product', 'pricing rule type error detail', 1, now(), now()),
('pPriceRoundName', 'zh-CN', '未知尾数处理：{value}', 200, 'product', 'pricing rounding error detail', 1, now(), now()),
('pPriceRoundName', 'en-US', 'Unknown rounding: {value}', 200, 'product', 'pricing rounding error detail', 1, now(), now()),
('pPriceFilterCtx', 'zh-CN', '筛选集范围必须指定工程', 200, 'product', 'pricing filter scope error detail', 1, now(), now()),
('pPriceFilterCtx', 'en-US', 'A filtered scope must specify the project', 200, 'product', 'pricing filter scope error detail', 1, now(), now()),
('pPriceFilterReq', 'zh-CN', '筛选集至少要给一个筛选条件', 200, 'product', 'pricing filter scope error detail', 1, now(), now()),
('pPriceFilterReq', 'en-US', 'A filtered scope needs at least one filter condition', 200, 'product', 'pricing filter scope error detail', 1, now(), now()),
('pPriceBatchSize', 'zh-CN', '{n} 个商品超过单批上限 {max}，请收紧筛选条件', 200, 'product', 'pricing batch limit error detail', 1, now(), now()),
('pPriceBatchSize', 'en-US', '{n} products exceed the per-batch limit of {max}; narrow the filter', 200, 'product', 'pricing batch limit error detail', 1, now(), now()),
('pTagObjectReq', 'zh-CN', '参数必须是 JSON 对象', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagObjectReq', 'en-US', 'Parameters must be a JSON object', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagArgReq', 'zh-CN', '{field} 必填', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagArgReq', 'en-US', '{field} is required', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagArgInt', 'zh-CN', '{field} 必须是整数', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagArgInt', 'en-US', '{field} must be an integer', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagArgNum', 'zh-CN', '{field} 必须是数字', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagArgNum', 'en-US', '{field} must be a number', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagArgRange', 'zh-CN', '{field} 必须在 {min}~{max} 之间，实际 {value}', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagArgRange', 'en-US', '{field} must be between {min} and {max}, got {value}', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagArgBounds', 'zh-CN', '{field} 必须在 {min}~{max} 之间', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagArgBounds', 'en-US', '{field} must be between {min} and {max}', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagExtraKeys', 'zh-CN', '不支持的参数键：{keys}', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagExtraKeys', 'en-US', 'Unsupported parameter keys: {keys}', 200, 'product', 'tag rule params error detail', 1, now(), now()),
('pTagPriceReq', 'zh-CN', 'minPrice 与 maxPrice 至少要给一个', 200, 'product', 'tag price range params error detail', 1, now(), now()),
('pTagPriceReq', 'en-US', 'Provide at least one of minPrice and maxPrice', 200, 'product', 'tag price range params error detail', 1, now(), now()),
('pTagPriceOrder', 'zh-CN', 'minPrice 不能大于 maxPrice', 200, 'product', 'tag price range params error detail', 1, now(), now()),
('pTagPriceOrder', 'en-US', 'minPrice cannot be greater than maxPrice', 200, 'product', 'tag price range params error detail', 1, now(), now()),
('pTagRuleName', 'zh-CN', '未知规则类型：{type}', 200, 'product', 'tag rule type error detail', 1, now(), now()),
('pTagRuleName', 'en-US', 'Unknown rule type: {type}', 200, 'product', 'tag rule type error detail', 1, now(), now()),
('pVarCountExcess', 'zh-CN', '{n} 个组合超过上限 {max}（请减少勾选的属性值或属性维度）', 200, 'product', 'variant combination limit error detail', 1, now(), now()),
('pVarCountExcess', 'en-US', '{n} combinations exceed the limit of {max} (reduce the selected attribute values or dimensions)', 200, 'product', 'variant combination limit error detail', 1, now(), now()),
('pVarDimExcess', 'zh-CN', '{n} 个维度超过上限 {max}（请减少参与变体的属性组）', 200, 'product', 'variant dimension limit error detail', 1, now(), now()),
('pVarDimExcess', 'en-US', '{n} dimensions exceed the limit of {max} (reduce the attribute groups used for variations)', 200, 'product', 'variant dimension limit error detail', 1, now(), now()),
('pVarAttrUnused', 'zh-CN', '属性组 {attribute} 未被该商品引用，或未标记为参与变体', 200, 'product', 'variant attribute error detail', 1, now(), now()),
('pVarAttrUnused', 'en-US', 'Attribute group {attribute} is not referenced by this product, or is not marked as a variation dimension', 200, 'product', 'variant attribute error detail', 1, now(), now()),
('pVarAttrOutside', 'zh-CN', '属性组 {attribute} 未参与该商品的变体', 200, 'product', 'variant attribute error detail', 1, now(), now()),
('pVarAttrOutside', 'en-US', 'Attribute group {attribute} does not participate in the variations of this product', 200, 'product', 'variant attribute error detail', 1, now(), now()),
('pVarValueOff', 'zh-CN', '属性组 {attribute} 下没有启用中的属性值 {value}', 200, 'product', 'variant attribute value error detail', 1, now(), now()),
('pVarValueOff', 'en-US', 'Attribute group {attribute} has no enabled value {value}', 200, 'product', 'variant attribute value error detail', 1, now(), now()),
('pRelatedSelf', 'zh-CN', '不能指向自己（{n} 个：{ids}）', 200, 'product', 'related product reference error detail', 1, now(), now()),
('pRelatedSelf', 'en-US', 'Cannot point to itself ({n}: {ids})', 200, 'product', 'related product reference error detail', 1, now(), now()),
('pRelatedMissing', 'zh-CN', '不存在（{n} 个：{ids}）', 200, 'product', 'related product reference error detail', 1, now(), now()),
('pRelatedMissing', 'en-US', 'Not found ({n}: {ids})', 200, 'product', 'related product reference error detail', 1, now(), now()),
('pRelatedScope', 'zh-CN', '不属于本工程（{n} 个：{ids}；每条形如 商品id@所属工程id）', 200, 'product', 'related product reference error detail', 1, now(), now()),
('pRelatedScope', 'en-US', 'Not in this project ({n}: {ids}; each entry is product-id@project-id)', 200, 'product', 'related product reference error detail', 1, now(), now()),
('pRefGuardBlock', 'zh-CN', '引用面 {columns}；命中 {n} 个商品、涉及 {projects} 个工程（{refs}）。请先在对应工程解绑后重试（守卫不自动清理）', 200, 'product', 'cross-project delete guard error detail', 1, now(), now()),
('pRefGuardBlock', 'en-US', 'Referenced by {columns}; {n} product(s) across {projects} project(s) ({refs}). Unbind them in their own projects first (the guard never auto-cleans)', 200, 'product', 'cross-project delete guard error detail', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('pPriceFilterReq', 'product.err.pricingFilterConditionRequired'), ('pPriceFilterCtx', 'product.err.pricingFilterProjectRequired'), ('pPriceArgNaN', 'product.err.pricingParamMissingOrNaN'), ('pPriceArgNum', 'product.err.pricingParamNotNumber'), ('pPriceArgRange', 'product.err.pricingParamRange'), ('pPriceArgBounds', 'product.err.pricingParamRangePlain'), ('pPriceArgReq', 'product.err.pricingParamRequired'), ('pPriceObjectReq', 'product.err.pricingParamsNotObject'), ('pPriceBatchSize', 'product.err.pricingTargetTooMany'), ('pPriceExtraKeys', 'product.err.pricingUnknownKeys'), ('pPriceRoundName', 'product.err.pricingUnknownRounding'), ('pPriceRuleName', 'product.err.pricingUnknownRuleType'), ('pRefGuardBlock', 'product.err.refGuardBlocked'), ('pRelatedScope', 'product.err.relatedCrossProject'), ('pRelatedMissing', 'product.err.relatedMissing'), ('pRelatedSelf', 'product.err.relatedSelfReference'), ('pTagArgInt', 'product.err.tagParamNotInteger'), ('pTagArgNum', 'product.err.tagParamNotNumber'), ('pTagArgRange', 'product.err.tagParamRange'), ('pTagArgBounds', 'product.err.tagParamRangePlain'), ('pTagArgReq', 'product.err.tagParamRequired'), ('pTagObjectReq', 'product.err.tagParamsNotObject'), ('pTagPriceReq', 'product.err.tagPriceNeedsOne'), ('pTagPriceOrder', 'product.err.tagPriceOrderWrong'), ('pTagExtraKeys', 'product.err.tagUnknownKeys'), ('pTagRuleName', 'product.err.tagUnknownRuleType'), ('pVarAttrOutside', 'product.err.variationAttributeNotInProduct'), ('pVarAttrUnused', 'product.err.variationAttributeUnreferenced'), ('pVarCountExcess', 'product.err.variationCountExceed'), ('pVarDimExcess', 'product.err.variationDimensionExceed'), ('pVarValueOff', 'product.err.variationValueNotEnabled')) AS renamed(new_key, old_key)
    JOIN sys_i18n source ON source.item_key = renamed.old_key AND source.lang = d.lang
    WHERE renamed.new_key = d.item_key)
ON CONFLICT (item_key, lang) DO NOTHING;

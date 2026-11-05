package migrations

func init() {
	registerSeed(Seed{
		Version:   "722-product-translation-short-keys-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `WITH keys(old_key, new_key, retire) AS (VALUES
    ('admin.product_translations.col.field', 'trField', true),
    ('admin.product_translations.col.origin', 'trOrigin', true),
    ('admin.product_translations.col.source', 'trSrc', true),
    ('admin.product_translations.col.state', 'trState', true),
    ('admin.product_translations.col.target', 'trTargetCol', true),
    ('admin.product_translations.empty', 'trEmpty', true),
    ('admin.product_translations.emptyTitle', 'trEmptyTitle', true),
    ('admin.product_translations.entityType.attribute', 'trTypeAttr', true),
    ('admin.product_translations.entityType.brand', 'trTypeBrand', true),
    ('admin.product_translations.entityType.category', 'trTypeCat', true),
    ('admin.product_translations.entityType.tag', 'trTypeTag', true),
    ('admin.product_translations.field.attributeValues', 'trFieldAttrVals', true),
    ('admin.product_translations.field.productDescription', 'trFieldProdDesc', true),
    ('admin.product_translations.field.productImageAlts', 'trProductAlts', true),
    ('admin.product_translations.field.productName', 'trFieldProdName', true),
    ('admin.product_translations.field.productSubtitle', 'trFieldProdSub', true),
    ('admin.product_translations.help.label', 'trHelp', true),
    ('admin.product_translations.label.lang', 'trLang', true),
    ('admin.product_translations.label.project', 'trProj', true),
    ('admin.product_translations.noTargetLang.action', 'trNoTgtLangAct', true),
    ('admin.product_translations.noTargetLang.title', 'trNoLangTitle', true),
    ('admin.product_translations.noscriptSwitch', 'trNoJsSwitch', true),
    ('admin.product_translations.ph.target', 'trTargetPh', true),
    ('admin.product_translations.richHint', 'trRichHint', true),
    ('admin.product_translations.saveAll', 'trSaveAll', true),
    ('admin.product_translations.saveHint', 'trSaveHint', true),
    ('admin.product_translations.state.missing', 'trStateMiss', true),
    ('admin.product_translations.state.translated', 'trStateDone', true),
    ('admin.product_translations.title', 'trTitle', true),
    ('admin.product_translations.viewProject', 'trViewProj', true)
)
SELECT CASE WHEN
 NOT EXISTS (SELECT 1 FROM keys k CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
 WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = langs.lang))
 AND NOT EXISTS (SELECT 1 FROM keys k JOIN sys_i18n s ON s.item_key = k.old_key
 WHERE k.retire OR NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang))
 AND NOT EXISTS (SELECT 1 FROM (VALUES ('trScopeInfo'), ('trProgress'), ('trGroupInfo'), ('trNoTargetDesc'), ('trSourceOnly')) AS complete(item_key)
 CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
 WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = complete.item_key AND t.lang = langs.lang))
 THEN 1 ELSE 0 END`,
		SQL: mustSQL("722_product_translation_short_keys_i18n.sql"),
	})
}

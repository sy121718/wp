package migrations

func init() {
	registerSeed(Seed{
		Version:   "696-product-mvc-facts-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('pTplNameReq'),
    ('pTplPathReq'),
    ('pTplPreviewNil'),
    ('pTplUnavailable'),
    ('seoCoverPartial'),
    ('seoModeGuide'),
    ('seoModeLanding')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("719d_current_696_product_mvc_facts_i18n.sql"),
	})
}

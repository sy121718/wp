package migrations

import "sync"

func registerProductListFilterI18n() {
	registerProductListFilterI18nOnce.Do(registerProductListFilterI18nSeed)
}

var registerProductListFilterI18nOnce sync.Once

func registerProductListFilterI18nSeed() {

	registerSeed(Seed{
		Version:   "411-i18n-product-list-filter",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('admin.common.filter.optionAll'),
    ('admin.common.filter.reset'),
    ('admin.common.filter.submit'),
    ('attrFltKw'),
    ('attrNoMatches'),
    ('brandFltKw'),
    ('brandNoMatches'),
    ('catFltKw'),
    ('catNoMatches'),
    ('tagFltKw'),
    ('tagNoMatches'),
    ('uiFltEmptyHint')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("721e_current_411_i18n_product_list_filter.sql"),
	})
}

func init() { registerProductListFilterI18n() }

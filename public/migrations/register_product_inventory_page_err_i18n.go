package migrations

import "sync"

func registerProductInventoryPageErrI18n() {
	registerProductInventoryPageErrI18nOnce.Do(registerProductInventoryPageErrI18nSeed)
}

var registerProductInventoryPageErrI18nOnce sync.Once

func registerProductInventoryPageErrI18nSeed() {

	registerSeed(Seed{
		Version:   "407-i18n-product-inventory-page-err",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('uiInternalErr'),
    ('admin.common.list.loadFailedDesc'),
    ('admin.common.list.loadFailedTitle'),
    ('tplDepsMiss')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("723x_current_407_i18n_product_inventory_page_err.sql"),
	})
}

func init() { registerProductInventoryPageErrI18n() }

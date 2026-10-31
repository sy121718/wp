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
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.product_detail_template.depsMissing'," +
			"'admin.common.list.loadFailedTitle','admin.common.list.loadFailedDesc'," +
			"'MsgInternalError')",
		SQL: mustSQL("407_i18n_product_inventory_page_err.sql"),
	})
}

func init() { registerProductInventoryPageErrI18n() }

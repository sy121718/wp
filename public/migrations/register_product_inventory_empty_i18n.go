package migrations

import "sync"

func registerProductInventoryEmptyI18n() {
	registerProductInventoryEmptyI18nOnce.Do(registerProductInventoryEmptyI18nSeed)
}

var registerProductInventoryEmptyI18nOnce sync.Once

func registerProductInventoryEmptyI18nSeed() {

	registerSeed(Seed{
		Version:   "402-i18n-product-inventory-empty",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 7 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.inventory.moves.emptyLead','admin.inventory.moves.emptyPurchaseLink'," +
			"'admin.inventory.moves.emptyTail','admin.inventory.list.emptyNew'," +
			"'admin.inventory_sources.list.emptyTitle','admin.inventory_sources.list.emptyNew'," +
			"'admin.inventory_sources.list.emptyFiltered')",
		SQL: mustSQL("402_i18n_product_inventory_empty.sql"),
	})
}

func init() { registerProductInventoryEmptyI18n() }

package migrations

import "sync"

func registerInventoryPaginationI18n() {
	registerInventoryPaginationI18nOnce.Do(registerInventoryPaginationI18nSeed)
}

var registerInventoryPaginationI18nOnce sync.Once

func registerInventoryPaginationI18nSeed() {

	registerSeed(Seed{
		Version:   "412-i18n-inventory-pagination",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.inventory.pagination.info'," +
			"'admin.inventory.pagination.more','admin.inventory.pagination.end')",
		SQL: mustSQL("412_i18n_inventory_pagination.sql"),
	})
}

func init() { registerInventoryPaginationI18n() }

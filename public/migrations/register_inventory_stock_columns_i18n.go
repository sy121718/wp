package migrations

import "sync"

func init() { registerInventoryStockColumnsI18n() }

var inventoryStockColumnsI18nOnce sync.Once

func registerInventoryStockColumnsI18n() {
	inventoryStockColumnsI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "639-inventory-stock-columns-i18n",
			TableName: "sys_i18n",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 8 THEN 1 ELSE 0 END FROM sys_i18n " +
				"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
				"'admin.inventory.sku.col.reserved', 'admin.inventory.sku.col.available', " +
				"'admin.inventory.filter.keyword', 'admin.inventory.create.warehousePh')",
			SQL: mustSQL("639_inventory_stock_columns_i18n.sql"),
		})
	})
}

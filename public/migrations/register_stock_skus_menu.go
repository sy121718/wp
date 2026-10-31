package migrations

import "sync"

func init() { registerStockSKUsMenu() }

var stockSKUsMenuOnce sync.Once

func registerStockSKUsMenu() {
	stockSKUsMenuOnce.Do(func() {
		registerSeed(Seed{
			Version:   "644-stock-skus-menu",
			TableName: "sys_menus",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_menus WHERE deleted_at IS NULL AND (" +
				"(type = 2 AND path = '/admin/inventory/skus') OR " +
				"(type = 3 AND title_key IN ('inventory.stock_create', 'inventory.stock_update')))",
			SQL: mustSQL("644_stock_skus_menu.sql"),
		})
	})
}

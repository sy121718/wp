package migrations

import "sync"

func init() { registerInventoryMovementsMenu() }

var inventoryMovementsMenuOnce sync.Once

func registerInventoryMovementsMenu() {
	inventoryMovementsMenuOnce.Do(func() {
		registerSeed(Seed{
			Version:      "648-inventory-movements-menu",
			TableName:    "sys_menus",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_menus WHERE deleted_at IS NULL AND type = 2 AND path = '/admin/inventory/movements'",
			SQL:          mustSQL("648_inventory_movements_menu.sql"),
		})
	})
}

package migrations

// register_inventory_movements_menu.go — 648 的注册项。
//
// 判据判的是「那一行在」：path = '/admin/inventory/movements' 且未软删。
// 不用「菜单表非空即跳过」—— 存量库菜单表恒非空，那样写这条种子永远不会跑。

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

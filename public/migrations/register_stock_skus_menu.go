package migrations

// register_stock_skus_menu.go — 644 的注册项。
//
// 判据判的是「这一批三行都在」：菜单一条（path）+ 两个按钮（title_key），
// 不用「菜单存在即跳过」—— 半成品（只建了菜单、按钮没建）会让页面既看不到入口
// 又看不到按钮，且永远不再重跑。

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

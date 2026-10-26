package migrations

// register_inventory_movements_page_i18n.go — 647 的注册项。
//
// 判据逐条枚举本批 12 个 key（中英成对共 24 行）：
// 用前缀 LIKE 会被将来同前缀的新 key 带跑；用全库总量会被同期其它批次的行满足。

import "sync"

func init() { registerInventoryMovementsPageI18n() }

var inventoryMovementsPageI18nOnce sync.Once

func registerInventoryMovementsPageI18n() {
	inventoryMovementsPageI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "647-inventory-movements-page-i18n",
			TableName: "sys_i18n",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 24 THEN 1 ELSE 0 END FROM sys_i18n " +
				"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
				"'admin.inventory.movements.title', 'admin.inventory.movements.lead', " +
				"'admin.inventory.movements.snapshot', 'admin.inventory.movements.timeHint', " +
				"'admin.inventory.movements.boundary', 'admin.inventory.movements.boundaryLink', " +
				"'admin.inventory.movements.boundaryTail', 'admin.inventory.movements.listLead', " +
				"'admin.inventory.movements.col.item', 'admin.inventory.movements.col.source', " +
				"'admin.inventory.movements.col.unitCost', 'admin.inventory.movements.emptyDesc')",
			SQL: mustSQL("647_inventory_movements_page_i18n.sql"),
		})
	})
}

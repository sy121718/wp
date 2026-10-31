package migrations

import "sync"

func registerInventoryMovesEmptyI18n() {
	registerInventoryMovesEmptyI18nOnce.Do(registerInventoryMovesEmptyI18nSeed)
}

var registerInventoryMovesEmptyI18nOnce sync.Once

func registerInventoryMovesEmptyI18nSeed() {

	registerSeed(Seed{
		Version:   "317-i18n-fix-inventory-moves-empty",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.inventory.moves.empty' " +
			"AND ((lang = 'zh-CN' AND item_value = '还没有库存流水 —— 入库请从采购入库页收货，盘点 / 报损请走右上角的库存调整。') " +
			"OR (lang = 'en-US' AND item_value = 'No stock movements yet — receive stock on the Purchase Receiving page; for counts or write-offs use Stock Adjustment in the top right.'))",
		SQL: mustSQL("317_fix_inventory_moves_empty_i18n.sql"),
	})
}

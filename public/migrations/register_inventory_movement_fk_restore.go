package migrations

// registerInventoryMovementFKRestore 登记 760：补回 173 重建分区表时丢掉的外键。
//
// 为什么单独一条迁移而不是改 173：历史迁移不回改（AGENTS.md），而且存量库已经
// 走完 173、需要一条幂等迁移把状态收敛到同一形状。
func registerInventoryMovementFKRestore() {
	register(Migration{
		Version:   "760-inventory-movement-fk-restore",
		TableName: "inventory_stock_movements",

		CheckSQL: `SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END
			FROM pg_constraint k
			JOIN pg_class c ON c.oid = k.conrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema()
			  AND c.relname = 'inventory_stock_movements'
			  AND k.conname = 'fk_inventory_movements_product'
			  AND k.contype = 'f'`,
		SQL: mustSQL("760_inventory_movement_fk_restore.sql"),
	})
}

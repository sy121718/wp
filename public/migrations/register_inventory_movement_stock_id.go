package migrations

import "sync"

func registerInventoryMovementStockID() {
	registerInventoryMovementStockIDOnce.Do(func() {
		register(Migration{
			Version:   "640-inventory-movement-stock-id",
			TableName: "inventory_stock_movements",
			CheckSQL: "SELECT CASE WHEN (" +
				"SELECT COUNT(*) FROM pg_attribute WHERE attrelid = to_regclass('inventory_stock_movements') " +
				"AND attname = 'stock_id' AND NOT attisdropped" +
				") <> 1 THEN 0 WHEN (" +
				"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() " +
				"AND tablename = 'inventory_stock_movements' AND indexname = 'idx_inventory_movements_stock_time'" +
				") <> 1 THEN 0 WHEN (" +
				"SELECT COUNT(*) FROM pg_constraint pc WHERE pc.conrelid = to_regclass('inventory_stock_movements') " +
				"AND pc.conname = 'inventory_stock_movements_stock_id_fkey'" +
				") <> 1 THEN 0 ELSE 1 END",

			SQL: mustSQL("640_inventory_movement_stock_id.sql"),
		})
	})
}

var registerInventoryMovementStockIDOnce sync.Once

func init() { registerInventoryMovementStockID() }

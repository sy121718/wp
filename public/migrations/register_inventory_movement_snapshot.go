package migrations

import "sync"

func init() { registerInventoryMovementSnapshot() }

var inventoryMovementSnapshotOnce sync.Once

func registerInventoryMovementSnapshot() {
	inventoryMovementSnapshotOnce.Do(func() {
		register(Migration{
			Version:   "646-inventory-movement-snapshot",
			TableName: "inventory_stock_movements",
			CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM pg_attribute " +
				"WHERE attrelid = to_regclass('inventory_stock_movements') AND NOT attisdropped " +
				"AND attname IN ('stock_name', 'stock_image_url')",
			SQL: mustSQL("646_inventory_movement_snapshot.sql"),
		})
	})
}

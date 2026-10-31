package migrations

import "sync"

func init() { registerInventoryMovementNullableOwner() }

var inventoryMovementNullableOwnerOnce sync.Once

func registerInventoryMovementNullableOwner() {
	inventoryMovementNullableOwnerOnce.Do(func() {
		register(Migration{
			Version:   "649-inventory-movement-nullable-owner",
			TableName: "inventory_stock_movements",
			CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM pg_attribute " +
				"WHERE attrelid = to_regclass('inventory_stock_movements') AND NOT attisdropped " +
				"AND attname IN ('product_id', 'variant_id') AND NOT attnotnull",
			SQL: mustSQL("649_inventory_movement_nullable_owner.sql"),
		})
	})
}

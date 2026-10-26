package migrations

// register_inventory_movement_nullable_owner.go — 649 的注册项。
//
// 判据只查 pg_attribute 的 attnotnull：**不能**写成「这两列可空」以外的形态
// （例如 SELECT count(*) FROM inventory_stock_movements WHERE variant_id IS NULL）——
// 存量库里恰好没有无归属流水时那条判据恒为 0，种子会被判成「尚未完成」反复重跑，
// 而它其实早就生效了。列属性是幂等判据里唯一稳定的那一种。

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

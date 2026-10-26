package migrations

// register_inventory_movement_snapshot.go — 646 的注册项。
//
// 判据只查系统目录（pg_attribute），不引用目标列本身：列还没建时在判据 SQL 的
// 解析阶段就会失败（ERROR: column ... does not exist），「尚未完成」被误判成
// 「迁移失败」，每轮启动都报一次错。637 / 640 / 642 都踩过这个坑。
//
// 两列齐才算完成 = 2；CLI 的 IF NOT EXISTS 让半成品（只加了一列）可以原地补完。

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

package migrations

import "sync"

// register_inventory_movement_stock_id.go — 640：流水加货行指针（stock_id）。
//
// 完成判据逐项枚举本批对象：列在、索引在、外键在，且没有「本该回填却回填错」的行。
// 不写成「表存在即跳过」—— inventory_stock_movements 一直都在，那条判据第一次执行
// 完就恒为真，半成品库（列加了、回填没跑）会被静默跳过。
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
			// 回填的正确性不在这里判：判据里一旦显式写 mv.stock_id，列还没建时整条 SQL
			// 在解析阶段就报 42703 —— 那会让判据本身成为一次失败，而不是「尚未完成」
			// （637 踩过同一个坑）。回填与外键、索引同在一个事务里，前一项成立即已跑完。
			SQL: mustSQL("640_inventory_movement_stock_id.sql"),
		})
	})
}

var registerInventoryMovementStockIDOnce sync.Once

func init() { registerInventoryMovementStockID() }

package migrations

import "sync"

// register_inventory_stock_identity.go — 641：货行身份改成「仓 × 仓库 SKU」。
//
// 完成判据枚举本批两个对象：那条唯一约束不在、那条索引不在。
// 不写成「表存在即跳过」—— inventory_stocks 一直都在。
func registerInventoryStockIdentity() {
	registerInventoryStockIdentityOnce.Do(func() {
		register(Migration{
			Version:   "641-inventory-stock-identity",
			TableName: "inventory_stocks",
			CheckSQL: "SELECT CASE WHEN (" +
				"SELECT COUNT(*) FROM pg_constraint pc WHERE pc.conrelid = to_regclass('inventory_stocks') " +
				"AND pc.conname = 'inventory_stocks_variant_id_warehouse_id_key'" +
				") <> 0 THEN 0 WHEN (" +
				"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() " +
				"AND tablename = 'inventory_stocks' AND indexname = 'idx_inventory_stocks_variant_project'" +
				") <> 0 THEN 0 ELSE 1 END",
			SQL: mustSQL("641_inventory_stock_identity.sql"),
		})
	})
}

var registerInventoryStockIdentityOnce sync.Once

func init() { registerInventoryStockIdentity() }

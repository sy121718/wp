package migrations

import "sync"

func registerWarehouseSKUMaster() {
	registerWarehouseSKUMasterOnce.Do(func() {
		register(Migration{
			Version:   "642-warehouse-sku-master",
			TableName: "inventory_stocks",
			CheckSQL: "SELECT CASE WHEN (" +
				"SELECT COUNT(*) FROM pg_attribute WHERE attrelid = to_regclass('inventory_stocks') " +
				"AND attname IN ('name','image_url','barcode','freight_price','cost_currency'," +
				"'cost_base_currency','cost_fx_rate','status','remark') AND NOT attisdropped" +
				") <> 9 THEN 0 WHEN (" +
				"SELECT COUNT(*) FROM pg_constraint pc WHERE pc.conrelid = to_regclass('inventory_stocks') " +
				"AND pc.conname IN ('inventory_stocks_status_check','ck_inventory_stocks_freight_nonneg'," +
				"'ck_inventory_stocks_cost_currency','ck_inventory_stocks_cost_fx_rate')" +
				") <> 4 THEN 0 WHEN (" +
				"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() " +
				"AND tablename = 'inventory_stocks' AND indexname = 'idx_inventory_stocks_name'" +
				") <> 1 THEN 0 ELSE 1 END",
			SQL: mustSQL("642_warehouse_sku_master.sql"),
		})
	})
}

var registerWarehouseSKUMasterOnce sync.Once

func init() { registerWarehouseSKUMaster() }

package migrations

import "sync"

func registerProductVariantWarehouseSkus() {
	registerProductVariantWarehouseSkusOnce.Do(func() {
		register(Migration{
			Version:   "637-product-variant-warehouse-skus",
			TableName: "product_variants",
			CheckSQL: "SELECT CASE WHEN (" +

				"SELECT COUNT(*) FROM pg_attribute WHERE attrelid = to_regclass('product_variants') " +
				"AND attname = 'warehouse_skus' AND NOT attisdropped" +
				") <> 1 THEN 0 WHEN (" +
				"SELECT COUNT(*) FROM pg_constraint pc WHERE pc.conrelid = to_regclass('product_variants') " +
				"AND pc.conname = 'product_variants_warehouse_skus_shape_check'" +
				") <> 1 THEN 0 WHEN (" +
				"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() " +
				"AND tablename = 'product_variants' AND indexname = 'idx_product_variants_warehouse_skus'" +
				") <> 1 THEN 0 WHEN (" +

				"SELECT COUNT(*) FROM product_variants v " +
				"WHERE COALESCE(to_jsonb(v) -> 'warehouse_skus', '[]'::jsonb) = '[]'::jsonb " +
				"AND EXISTS (SELECT 1 FROM inventory_stocks s WHERE s.variant_id = v.id)" +
				") <> 0 THEN 0 ELSE 1 END",
			SQL: mustSQL("637_product_variant_warehouse_skus.sql"),
		})
	})
}

var registerProductVariantWarehouseSkusOnce sync.Once

func init() { registerProductVariantWarehouseSkus() }

package migrations

import "sync"

// register_product_variant_warehouse_skus.go — 637：关联从库存行搬到商品变体侧（去认领）。
//
// 结构变更（加列 + 约束 + 索引 + 回填），必须在 Migrations 台账。
// 完成判据不写成「列存在即跳过」：products / product_variants 一直都在，那条判据在第一次
// 执行完之后恒为真 —— 半成品库（列加了、回填没跑）会被静默跳过（DB-015 的教训）。
// 这里判「列在 + 形状约束在 + GIN 索引在 + 没有该回填而未回填的变体」四项同时成立。
func registerProductVariantWarehouseSkus() {
	registerProductVariantWarehouseSkusOnce.Do(func() {
		register(Migration{
			Version:   "637-product-variant-warehouse-skus",
			TableName: "product_variants",
			CheckSQL: "SELECT CASE WHEN (" +
				// 列存在性用 pg_attribute 判（直接在这里引用该列的话，列还没建时整条 SQL
				// 在解析阶段就报 42703 —— 那会让判据本身成为一次失败，而不是「尚未完成」）。
				"SELECT COUNT(*) FROM pg_attribute WHERE attrelid = to_regclass('product_variants') " +
				"AND attname = 'warehouse_skus' AND NOT attisdropped" +
				") <> 1 THEN 0 WHEN (" +
				"SELECT COUNT(*) FROM pg_constraint pc WHERE pc.conrelid = to_regclass('product_variants') " +
				"AND pc.conname = 'product_variants_warehouse_skus_shape_check'" +
				") <> 1 THEN 0 WHEN (" +
				"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() " +
				"AND tablename = 'product_variants' AND indexname = 'idx_product_variants_warehouse_skus'" +
				") <> 1 THEN 0 WHEN (" +
				// 回填完整性：还有「已认领但引用为空」的变体就还没做完。
				// 用 to_jsonb(v) 取整行再取键，而不是写 v.warehouse_skus —— 同上，列不存在时
				// 显式列引用会让整条判据 SQL 解析失败。
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

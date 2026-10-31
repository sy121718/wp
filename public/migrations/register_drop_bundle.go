package migrations

import "sync"

func registerDropBundle() {
	registerDropBundleOnce.Do(registerDropBundleSeed)
}

var registerDropBundleOnce sync.Once

func registerDropBundleSeed() {

	register(Migration{
		Version:   "636-drop-bundle",
		TableName: "products",
		CheckSQL: "SELECT CASE WHEN (" +
			"SELECT COUNT(*) FROM pg_constraint pc WHERE pc.conrelid = to_regclass('products') " +
			"AND pc.conname IN ('products_bundle_items_shape_check', 'products_type_check', 'ck_products_bundle_price_positive')" +
			") + (" +
			"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() " +
			"AND table_name = 'products' AND column_name IN ('bundle_items', 'type')" +
			") + (" +
			"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() " +
			"AND tablename = 'products' AND indexname = 'idx_products_bundle_items_gin'" +
			") + (CASE WHEN to_regclass('inventory_bom_items') IS NULL THEN 0 ELSE 1 END) = 0 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("636_drop_bundle.sql"),
	})

	registerSeed(Seed{
		Version:   "636b-retire-bundle-i18n-menu",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (" +
			"SELECT COUNT(*) FROM sys_i18n WHERE item_key LIKE 'admin.product_bundle.%' " +
			"OR item_key LIKE 'admin.product_detail.bundle.%' OR item_key LIKE 'site.fragment.bundle.%' " +
			"OR item_key LIKE 'ErrBundle%' OR item_key LIKE 'MsgBundle%' " +
			"OR item_key LIKE 'BundleMember%' OR item_key LIKE 'BundleSource%'" +
			") + (" +
			"SELECT COUNT(*) FROM sys_i18n WHERE item_key IN (" +
			"'admin.products.label.type','admin.products.type.variant','admin.products.type.bundle'," +
			"'admin.products.edit.typeNote','admin.products.edit.bundleHint','admin.products.ph.skuBundle'," +
			"'admin.products.sku.bundleHint','admin.products.sku.bundleManual','admin.products.sku.regenerate'," +
			"'ErrProductTypeInvalid','ErrProductTypeImmutable','ErrBundlePriceRequired','ErrBundleSKURequired'," +
			"'VariantSkipBundleReferenced','ErrWarehouseSKUBundleNotAllowed')" +
			") + (" +
			"SELECT COUNT(*) FROM sys_menus WHERE path = '/admin/products/bundle' AND deleted_at IS NULL" +
			") = 0 THEN 1 ELSE 0 END",
		SQL: mustSQL("636b_retire_bundle_i18n_menu.sql"),
	})
}

func init() { registerDropBundle() }

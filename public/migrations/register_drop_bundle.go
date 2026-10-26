package migrations

import "sync"

// register_drop_bundle.go — 636 / 636b：捆绑品（bundle）概念整体下线的 DB 侧收尾。
//
// 见 636_drop_bundle.sql 与 636b_retire_bundle_i18n_menu.sql 的文件头。两半分开注册是刻意的：
//
//	· 636 是结构变更（DROP 表 / 列 / 约束 / 索引 + 删权限点），必须在 **Migrations** 台账；
//	· 636b 是数据清理（sys_i18n 词条 + sys_menus 菜单行），必须在 **Seeds** 台账且版本号
//	  大于所有写过这些行的 seed（156 / 191 / 232 / 247 / 252 / 314 / 315 / 430 / 449）——
//	  否则同一轮 RunSeeds 里会出现「先删后插」，净结果是这些行又回来了（122 / 418 / 419 / 458
//	  的实测形态：Migrations 台账的删除永远赢不过在 Seeds 台账里重建它的 seed）。
//
// 注册方式：本文件自带 init()（与 register_retire_seo_grade_keys.go 同形）。
func registerDropBundle() {
	registerDropBundleOnce.Do(registerDropBundleSeed)
}

// registerDropBundleOnce 让重复调用成为空操作。
var registerDropBundleOnce sync.Once

// registerDropBundleSeed 注册 636（结构）与 636b（数据）。
func registerDropBundleSeed() {
	// 636 的完成判据：捆绑的形状约束一条都不剩（含「列已删、约束跟着走」的自然收敛）。
	// 为什么不用「表存在即跳过」的默认形态：products 表当然一直存在，默认判据在第一条迁移
	// 执行完之后恒为真 —— 那会让本迁移在「列删了、约束没删」的半成品库上被静默跳过（DB-015 的教训）。
	//
	// CheckSQL 由迁移器 db.Raw 直接执行、没有任何参数替换：要判定的名字只能写在 SQL 字面量里
	//（TableName 只进台账，写成名字 = ? 永远查不到行，178 踩过）。
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

	// 636b 的完成判据：本批要清的行一行不剩。
	// 与 458 同一取舍（「宁可重跑，不可静默跳过」）：只有删除完成后条件才成立；
	// 半成品库（删了一部分）会重跑，重复执行只是多跑一次幂等 DELETE。
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

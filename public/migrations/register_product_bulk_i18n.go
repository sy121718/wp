package migrations

// register_product_bulk_i18n.go — 迁移 615 的 seed 注册（商品写表单的界面词条）。
//
// 门槛判据**枚举本批自己的 34 个 item_key**（上界封闭，34 × 2 语言 = 68 行）：
// 用 LIKE 前缀会在「别批次已有同前缀行」时计数虚高、本批被静默跳过（058 的真实故障），
// 也会在将来新增同前缀 key 时永远追不平、每次启动都重跑（076 的真实故障）。
//
// 注册方式：本文件自带 func init()（与 466a / 606 一致），不在 register.go 的 init() 里再加一行。
func init() {
	registerSeed(Seed{
		Version:   "615-product-bulk-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 68 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.common.write.refresh', 'admin.product_attributes.bulk.noneSelected', " +
			"'admin.product_attributes.bulkDelete.allDeleted', 'admin.product_attributes.bulkDelete.allSkipped', " +
			"'admin.product_attributes.bulkDelete.partial', 'admin.product_attributes.bulkDelete.refresh', " +
			"'admin.product_attributes.list.deleted', 'admin.product_attributes.saved', " +
			"'admin.product_attributes.values.saved', 'admin.product_brands.bulk.noneSelected', " +
			"'admin.product_brands.bulkDelete.allDeleted', 'admin.product_brands.bulkDelete.allSkipped', " +
			"'admin.product_brands.bulkDelete.partial', 'admin.product_brands.bulkDelete.refresh', " +
			"'admin.product_categories.bulk.noneSelected', 'admin.product_categories.bulkDelete.allDeleted', " +
			"'admin.product_categories.bulkDelete.allSkipped', 'admin.product_categories.bulkDelete.partial', " +
			"'admin.product_categories.bulkDelete.refresh', 'admin.product_tags.bulk.noneSelected', " +
			"'admin.product_tags.bulkDelete.allDeleted', 'admin.product_tags.bulkDelete.allSkipped', " +
			"'admin.product_tags.bulkDelete.partial', 'admin.product_tags.bulkDelete.refresh', " +
			"'admin.product_tags.list.deleted', 'admin.product_tags.saved', " +
			"'admin.products.bulk.noneSelected', 'admin.products.bulkPricing.allSkipped', " +
			"'admin.products.bulkPricing.allUpdated', 'admin.products.bulkPricing.noneSelected', " +
			"'admin.products.bulkPricing.partial', 'admin.products.bulkResult.allDeleted', " +
			"'admin.products.bulkResult.allSkipped', 'admin.products.bulkResult.partial'" +
			")",
		SQL: mustSQL("615_product_bulk_ui_i18n.sql"),
	})
}

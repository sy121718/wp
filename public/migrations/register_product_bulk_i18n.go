package migrations

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

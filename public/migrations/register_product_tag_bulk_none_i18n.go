package migrations

func init() {
	registerSeed(Seed{
		Version:   "736-product-tag-bulk-none-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (('tagBulkNoneSel', 'zh-CN'), ('tagBulkNoneSel', 'en-US'))`,
		SQL: mustSQL("736_product_tag_bulk_none_i18n.sql"),
	})
}

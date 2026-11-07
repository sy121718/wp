package migrations

func init() {
	registerSeed(Seed{
		Version:   "751-purchase-option-text-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*)=6 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('admin.inventory_purchases.option.cost','zh-CN'),('admin.inventory_purchases.option.cost','en-US'),
  ('admin.inventory_purchases.option.costTail','zh-CN'),('admin.inventory_purchases.option.costTail','en-US'),
  ('admin.inventory_purchases.option.warehouseDefaultSuffix','zh-CN'),
  ('admin.inventory_purchases.option.warehouseDefaultSuffix','en-US'))`,
		SQL: mustSQL("751_purchase_option_text_i18n.sql"),
	})
}

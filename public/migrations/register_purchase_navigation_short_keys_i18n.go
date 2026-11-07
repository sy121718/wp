package migrations

func init() {
	registerSeed(Seed{
		Version:   "750-purchase-navigation-short-keys-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*)=10 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('admin.inventory_sources.status.active','zh-CN'),('admin.inventory_sources.status.active','en-US'),
  ('admin.inventory_sources.status.disabled','zh-CN'),('admin.inventory_sources.status.disabled','en-US'),
  ('admin.inventory_sources.related.typeDefault','zh-CN'),('admin.inventory_sources.related.typeDefault','en-US'),
  ('admin.navigations.panel.created','zh-CN'),('admin.navigations.panel.created','en-US'),
  ('admin.navigations.panel.open','zh-CN'),('admin.navigations.panel.open','en-US'))`,
		SQL: mustSQL("750_purchase_navigation_short_keys_i18n.sql"),
	})
}

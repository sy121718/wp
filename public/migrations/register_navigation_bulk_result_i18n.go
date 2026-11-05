package migrations

func init() {
	registerSeed(Seed{
		Version:   "740-navigation-bulk-result-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 8 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('navBulkNone', 'zh-CN'), ('navBulkNone', 'en-US'),
  ('navBulkSkip', 'zh-CN'), ('navBulkSkip', 'en-US'),
  ('navBulkDone', 'zh-CN'), ('navBulkDone', 'en-US'),
  ('navBulkPart', 'zh-CN'), ('navBulkPart', 'en-US'))`,
		SQL: mustSQL("740_navigation_bulk_result_i18n.sql"),
	})
}

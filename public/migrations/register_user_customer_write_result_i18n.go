package migrations

func init() {
	registerSeed(Seed{
		Version:   "731-user-customer-write-result-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('custNoneSel', 'zh-CN'), ('custNoneSel', 'en-US'),
  ('custBulkStat', 'zh-CN'), ('custBulkStat', 'en-US'),
  ('custBulkUnlock', 'zh-CN'), ('custBulkUnlock', 'en-US'))`,
		SQL: mustSQL("731_user_customer_write_result_i18n.sql"),
	})
}

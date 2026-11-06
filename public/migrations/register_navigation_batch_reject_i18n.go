package migrations

func init() {
	registerSeed(Seed{
		Version:   "748-navigation-batch-reject-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*)=2 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (('navBatchReject','zh-CN'),('navBatchReject','en-US'))`,
		SQL: mustSQL("748_navigation_batch_reject_i18n.sql"),
	})
}

package migrations

func init() {
	registerSeed(Seed{
		Version:   "732-membership-write-result-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (('memRecalcDone', 'zh-CN'), ('memRecalcDone', 'en-US'))`,
		SQL: mustSQL("732_membership_write_result_i18n.sql"),
	})
}

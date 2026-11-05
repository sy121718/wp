package migrations

func init() {
	registerSeed(Seed{
		Version:   "733-navigation-write-result-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (('navTrSaved', 'zh-CN'), ('navTrSaved', 'en-US'))`,
		SQL: mustSQL("733_navigation_write_result_i18n.sql"),
	})
}

package migrations

func init() {
	registerSeed(Seed{
		Version:   "729-contenttemplate-delete-result-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (('ctplDeleteDone', 'zh-CN'), ('ctplDeleteDone', 'en-US'))`,
		SQL: mustSQL("729_contenttemplate_delete_result_i18n.sql"),
	})
}

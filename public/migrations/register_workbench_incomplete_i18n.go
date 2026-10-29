package migrations

func init() {
	registerSeed(Seed{
		Version:      "678-workbench-incomplete-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang IN ('zh-CN','en-US') AND item_key = 'workbench.preview.incomplete'",
		SQL:          mustSQL("678_workbench_incomplete_i18n.sql"),
	})
}

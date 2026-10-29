package migrations

func init() {
	registerSeed(Seed{
		Version: "682-workbench-preview-readonly-i18n", TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang IN ('zh-CN','en-US') AND item_key = 'workbench.preview.readonly'",
		SQL:          mustSQL("682_workbench_preview_readonly_i18n.sql"),
	})
}

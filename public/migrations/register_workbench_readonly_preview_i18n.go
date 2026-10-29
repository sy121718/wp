package migrations

func init() {
	registerSeed(Seed{
		Version:      "685-workbench-readonly-preview-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang IN ('zh-CN','en-US') AND item_key = 'workbench.preview.readonly'",
		SQL:          mustSQL("685_workbench_readonly_preview_i18n.sql"),
	})
}

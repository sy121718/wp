package migrations

func init() {
	register(Migration{
		Version: "688-published-action-dependencies", TableName: "page_dependencies",
		CheckSQL: `SELECT CASE WHEN COUNT(*)=2 THEN 1 ELSE 0 END FROM pg_constraint WHERE conname IN ('page_dependencies_dependency_kind_check','presentation_dependencies_dependency_kind_check') AND conrelid IN (to_regclass('page_dependencies'),to_regclass('presentation_dependencies')) AND POSITION('page_target' IN pg_get_constraintdef(oid))>0 AND POSITION('presentation_target' IN pg_get_constraintdef(oid))>0`,
		SQL:      mustSQL("688_published_action_dependencies.sql"),
	})
	registerSeed(Seed{
		Version: "688a-published-action-in-use-i18n", TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*)=4 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang IN ('zh-CN','en-US') AND item_key IN ('page.err.actionTargetInUse','presentation.err.actionTargetInUse')`,
		SQL:          mustSQL("688a_published_action_in_use_i18n.sql"),
	})
}

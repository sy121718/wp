package migrations

func init() {
	registerSeed(Seed{
		Version: "695-workbench-action-target-availability-i18n", TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'workbench.err.previewActionTargetUnavailable' AND lang = 'zh-CN') AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'workbench.err.previewActionTargetUnavailable' AND lang = 'en-US') AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'workbench.ui.editor.target-unavailable' AND lang = 'zh-CN') AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'workbench.ui.editor.target-unavailable' AND lang = 'en-US') AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'workbench.ui.editor.target-reselect' AND lang = 'zh-CN') AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'workbench.ui.editor.target-reselect' AND lang = 'en-US') THEN 1 ELSE 0 END`,
		SQL:          mustSQL("695_workbench_action_target_availability_i18n.sql"),
	})
}

package migrations

import "sync"

func registerWorkbenchFacingI18n() {
	registerWorkbenchFacingI18nOnce.Do(registerWorkbenchFacingI18nSeed)
}

var registerWorkbenchFacingI18nOnce sync.Once

func registerWorkbenchFacingI18nSeed() {

	registerSeed(Seed{
		Version:   "292-workbench-facing-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 8 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'workbench.err.structureTemplateFieldBinding', 'workbench.err.previewFieldBindingUnsupported', " +
			"'workbench.err.previewDocumentInvalid', 'workbench.err.templateEntityTypeMismatch', " +
			"'workbench.err.templateFieldBindingInvalid', 'workbench.err.templateProjectScope', " +
			"'workbench.err.instanceNoDocument', 'workbench.err.instanceSaveRejected') " +
			"AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'MsgCompileFailed' AND lang = 'en-US')",
		SQL: mustSQL("292_workbench_facing_i18n.sql"),
	})
}

package migrations

import "sync"

// registerWorkbenchGlobalSaveFailedI18n 注册 756：工作台全局主题面板的保存失败提示。
func registerWorkbenchGlobalSaveFailedI18n() {
	registerWorkbenchGlobalSaveFailedI18nOnce.Do(registerWorkbenchGlobalSaveFailedI18nSeed)
}

var registerWorkbenchGlobalSaveFailedI18nOnce sync.Once

func registerWorkbenchGlobalSaveFailedI18nSeed() {
	registerSeed(Seed{
		Version:   "756-workbench-global-save-failed-i18n",
		TableName: "sys_i18n",
		// 门槛逐条枚举本批的两个 (item_key, lang)：中英成对才算完成。
		// 全库计数或 LIKE 前缀会让存量库永远满足，补词条的那条迁移就再也不会执行。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('workbench.ui.settings.saveFailed', 'zh-CN'), " +
			"('workbench.ui.settings.saveFailed', 'en-US'))",
		SQL: mustSQL("756_workbench_global_save_failed_i18n.sql"),
	})
}

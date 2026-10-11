package migrations

import "sync"

var registerAdminPluginWriteUII18nOnce sync.Once

func registerAdminPluginWriteUII18n() {
	registerAdminPluginWriteUII18nOnce.Do(func() {
		// Only these four unique key/language pairs can satisfy this guard.
		registerSeed(Seed{
			Version:   "608-admin-plugin-write-ui-i18n",
			TableName: "sys_i18n",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
				"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
				"'admin.plugins.write.noJs', '服务器内部错误，请稍后重试')",
			SQL: mustSQL("608_admin_plugin_write_ui_i18n.sql"),
		})
	})
}

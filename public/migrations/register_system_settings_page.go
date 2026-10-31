package migrations

import "sync"

func registerSystemSettingsPage() {
	registerSystemSettingsPageOnce.Do(registerSystemSettingsPageSeed)
}

var registerSystemSettingsPageOnce sync.Once

func registerSystemSettingsPageSeed() {
	registerSeed(Seed{
		Version:   "497-system-settings-page",
		TableName: "sys_config",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_config WHERE group_key = 'trade') >= 1 " +
			"AND (SELECT COUNT(*) FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.system.title','admin.system.submit','admin.system.field.default_lang','admin.system.field.default_currency'" +
			")) >= 4 THEN 1 ELSE 0 END",
		SQL: mustSQL("497_system_settings_page.sql"),
	})
}

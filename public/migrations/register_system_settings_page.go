package migrations

import "sync"

// register_system_settings_page.go — 系统设置页：trade 组 + 本页词条（497）。
func registerSystemSettingsPage() {
	registerSystemSettingsPageOnce.Do(registerSystemSettingsPageSeed)
}

// registerSystemSettingsPageOnce 让重复调用成为空操作。
var registerSystemSettingsPageOnce sync.Once

// registerSystemSettingsPageSeed 注册 497（真正干活的那一半）。
//
// 判定按本批自己的对象逐条枚举（上界封闭）：组存在 + 四个页面键的 zh-CN 词条齐了才算完成。
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，值只能写进 SQL 字面量。
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

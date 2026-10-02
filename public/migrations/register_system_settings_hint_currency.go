package migrations

import "sync"

// register_system_settings_hint_currency.go — 系统设置页 hint 改准（499）。
func registerSystemSettingsHintCurrency() {
	registerSystemSettingsHintCurrencyOnce.Do(registerSystemSettingsHintCurrencySeed)
}

// registerSystemSettingsHintCurrencyOnce 让重复调用成为空操作。
var registerSystemSettingsHintCurrencyOnce sync.Once

// registerSystemSettingsHintCurrencySeed 注册 499（真正干活的那一半）。
func registerSystemSettingsHintCurrencySeed() {
	registerSeed(Seed{
		Version:   "499-system-settings-hint-currency",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key IN (" +
			"'admin.system.hint.trade','admin.system.help.trade'" +
			") AND item_value LIKE '%多币种%' OR item_key IN (" +
			"'admin.system.hint.trade','admin.system.help.trade'" +
			") AND item_value LIKE '%multi-currency%'",
		SQL: mustSQL("499_system_settings_hint_currency.sql"),
	})
}

package migrations

import "sync"

func registerSystemSettingsHintCurrencyNoConversion() {
	registerSystemSettingsHintCurrencyNoConversionOnce.Do(registerSystemSettingsHintCurrencyNoConversionSeed)
}

var registerSystemSettingsHintCurrencyNoConversionOnce sync.Once

func registerSystemSettingsHintCurrencyNoConversionSeed() {

	registerSeed(Seed{
		Version:   "503-system-settings-hint-currency-no-conversion",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.system.hint.trade','admin.system.help.trade') " +
			"AND (item_value LIKE '%不换算金额%' OR item_value LIKE '%does not convert amounts%')",
		SQL: mustSQL("503_system_settings_hint_currency_no_conversion.sql"),
	})
}

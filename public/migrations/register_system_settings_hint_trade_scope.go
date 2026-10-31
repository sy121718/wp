package migrations

import "sync"

func registerSystemSettingsHintTradeScope() {
	registerSystemSettingsHintTradeScopeOnce.Do(registerSystemSettingsHintTradeScopeSeed)
}

var registerSystemSettingsHintTradeScopeOnce sync.Once

func registerSystemSettingsHintTradeScopeSeed() {

	registerSeed(Seed{
		Version:   "500-system-settings-hint-trade-scope",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.system.hint.trade','admin.system.help.trade') " +
			"AND (item_value LIKE '%没有货币选择器%' OR item_value LIKE '%no currency picker%' OR " +
			"item_value LIKE '%改的是地区，不是货币%' OR item_value LIKE '%change their region, not the currency%')",
		SQL: mustSQL("500_system_settings_hint_trade_scope.sql"),
	})
}

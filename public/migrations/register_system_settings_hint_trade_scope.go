package migrations

import "sync"

// register_system_settings_hint_trade_scope.go — trade 提示按产品口径改准（500）。
func registerSystemSettingsHintTradeScope() {
	registerSystemSettingsHintTradeScopeOnce.Do(registerSystemSettingsHintTradeScopeSeed)
}

// registerSystemSettingsHintTradeScopeOnce 让重复调用成为空操作。
var registerSystemSettingsHintTradeScopeOnce sync.Once

// registerSystemSettingsHintTradeScopeSeed 注册 500（真正干活的那一半）。
func registerSystemSettingsHintTradeScopeSeed() {
	// 判定按本批自己的 key 逐条枚举：两条 key × 两语言都是新值才跳过。
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

package migrations

import "sync"

// register_system_settings_hint_currency_no_conversion.go — trade 提示补「改币种不换算金额」（503）。
//
// 号段说明：本批原用 501，与并行的 `501_order_ship_country.sql`（Migrations 台账）撞号。
// 两条链各自排序、互不校验，撞号当下不会报错 —— 但同号会让「改动时把一条从 seed 改成
// migration」直接触发「迁移版本重复」启动失败，属埋雷。让号到 503（Migrations 那条已进
// make migrate 日志，不宜再动）。
func registerSystemSettingsHintCurrencyNoConversion() {
	registerSystemSettingsHintCurrencyNoConversionOnce.Do(registerSystemSettingsHintCurrencyNoConversionSeed)
}

// registerSystemSettingsHintCurrencyNoConversionOnce 让重复调用成为空操作。
var registerSystemSettingsHintCurrencyNoConversionOnce sync.Once

// registerSystemSettingsHintCurrencyNoConversionSeed 注册 501（真正干活的那一半）。
func registerSystemSettingsHintCurrencyNoConversionSeed() {
	// 判定按本批自己的 key 逐条枚举：两条 key × 两语言都是新值才跳过。
	//
	// 特征串只取本批**新增**的那句（「不换算金额」/「does not convert amounts」）：
	// 不要复用 500 的四个特征串 —— 那四句在新旧文案里都在，判不出本批是否已应用，
	// 结果是条件恒真、每次启动都重跑。
	registerSeed(Seed{
		Version:   "503-system-settings-hint-currency-no-conversion",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.system.hint.trade','admin.system.help.trade') " +
			"AND (item_value LIKE '%不换算金额%' OR item_value LIKE '%does not convert amounts%')",
		SQL: mustSQL("503_system_settings_hint_currency_no_conversion.sql"),
	})
}

package migrations

import "sync"

// register_settings_locale_mode_hint_i18n.go — 站点设置页「语言方案说明」的文案覆盖（493）。
//
// 词条 admin.settings.locales.hint.mode_intro 原先指向已删除的 config.yaml 键
// （i18n.site_lang_url_mode）。新口径：该开关**按工程**在站点设置页配，sys_config 的
// i18n 组只是「工程未配置时的全局默认」。
//
// 注册方式：由 register.go 的 init() 显式调用（与 316 / 317 / 492 同形）。
func registerSettingsLocaleModeHintI18n() {
	registerSettingsLocaleModeHintI18nOnce.Do(registerSettingsLocaleModeHintI18nSeed)
}

// registerSettingsLocaleModeHintI18nOnce 让重复调用成为空操作。
var registerSettingsLocaleModeHintI18nOnce sync.Once

// registerSettingsLocaleModeHintI18nSeed 注册 493（真正干活的那一半）。
func registerSettingsLocaleModeHintI18nSeed() {
	// 493：1 个 key × 2 语言。
	//
	// 门槛判据按**本批自己的 key 逐条枚举**（key 写死 + 两种语言精确值比较）：
	// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，值只能写进 SQL 字面量。
	// 上界封闭：不用 LIKE 前缀、不用全库总量。
	registerSeed(Seed{
		Version:   "493-i18n-settings-locale-mode-hint",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.settings.locales.hint.mode_intro' AND item_value IN (" +
			"'访问路径的语言方案由本页的「语言 URL 方案」开关控制（工程级，保存即生效）；未配置时跟随系统配置里的全局默认。'," +
			"'The language URL scheme is controlled by the \"Language URL scheme\" switch on this page (per project, effective on save); when unset it follows the global default in the system config.'" +
			")",
		SQL: mustSQL("493_settings_locale_mode_hint_i18n.sql"),
	})
}

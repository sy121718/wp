package migrations

import "sync"

func registerSettingsLocaleModeHintI18n() {
	registerSettingsLocaleModeHintI18nOnce.Do(registerSettingsLocaleModeHintI18nSeed)
}

var registerSettingsLocaleModeHintI18nOnce sync.Once

func registerSettingsLocaleModeHintI18nSeed() {

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

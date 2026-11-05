package migrations

import "sync"

// registerThemeSettingsCustomizeI18n 登记主题设置「块自定义」按钮的两条界面词条（714）。
//
// 这两条 key 一直被 `internal/templates/admin/project/theme_settings.html` 引用，却从未
// 进过词库 —— 英文界面下看到的是模板里的中文兜底。它们纯属界面文案（按钮与提示），
// 与业务错误声明无关，所以只在这里 seed，不进模块 enums。
func registerThemeSettingsCustomizeI18n() {
	registerThemeSettingsCustomizeI18nOnce.Do(registerThemeSettingsCustomizeI18nSeed)
}

var registerThemeSettingsCustomizeI18nOnce sync.Once

func registerThemeSettingsCustomizeI18nSeed() {
	registerSeed(Seed{
		Version:   "714-theme-settings-customize-i18n",
		TableName: "sys_i18n",
		// 门槛只枚举本批自己的 (item_key, lang) 四个元组：两条 key 的两个语言全齐才算完成。
		// 用全库计数或 LIKE 前缀会让存量库永远满足，补词条的那条迁移就再也不会执行；
		// 只数 zh-CN 也不行 —— en-US 缺的时候门槛照样成立，英文界面就永远缺词。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('admin.theme_settings.customize', 'zh-CN'), " +
			"('admin.theme_settings.customize', 'en-US'), " +
			"('admin.theme_settings.customize_empty', 'zh-CN'), " +
			"('admin.theme_settings.customize_empty', 'en-US'))",
		SQL: mustSQL("714_theme_settings_customize_i18n.sql"),
	})
}

package migrations

import "sync"

func registerFragmentLoginPanelI18n() {
	registerFragmentLoginPanelI18nOnce.Do(registerFragmentLoginPanelI18nSeed)
}

var registerFragmentLoginPanelI18nOnce sync.Once

func registerFragmentLoginPanelI18nSeed() {

	registerSeed(Seed{
		Version:   "293-fragment-login-panel-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'site.fragment.login_panel.login_register', 'site.fragment.login_panel.continue_shopping')",
		SQL: mustSQL("293_fragment_login_panel_i18n.sql"),
	})
}

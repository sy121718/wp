package migrations

import "sync"

func registerAdminI18nOptionUIAvailable() {
	registerAdminI18nOptionUIAvailableOnce.Do(registerAdminI18nOptionUIAvailableSeed)
}

var registerAdminI18nOptionUIAvailableOnce sync.Once

func registerAdminI18nOptionUIAvailableSeed() {

	registerSeed(Seed{
		Version:   "502-admin-i18n-option-ui-available",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.i18n.option.ui_available' AND lang = 'en-US'",
		SQL: mustSQL("502_admin_i18n_option_ui_available.sql"),
	})
}

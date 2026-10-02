package migrations

import "sync"

// register_admin_i18n_option_ui_available.go — i18n 页语言标记词条（502）。
func registerAdminI18nOptionUIAvailable() {
	registerAdminI18nOptionUIAvailableOnce.Do(registerAdminI18nOptionUIAvailableSeed)
}

// registerAdminI18nOptionUIAvailableOnce 让重复调用成为空操作。
var registerAdminI18nOptionUIAvailableOnce sync.Once

// registerAdminI18nOptionUIAvailableSeed 注册 502（真正干活的那一半）。
func registerAdminI18nOptionUIAvailableSeed() {
	// 门槛挑 **en-US** 行（= 1 行）：中文行与模板里的 t() 兜底同形，容易被别处顺手加上，
	// 按 zh-CN 计数会让门槛在「本批还没跑」时就成立、词条被静默跳过（416 / 431 / 435 的同一理由）。
	registerSeed(Seed{
		Version:   "502-admin-i18n-option-ui-available",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.i18n.option.ui_available' AND lang = 'en-US'",
		SQL: mustSQL("502_admin_i18n_option_ui_available.sql"),
	})
}

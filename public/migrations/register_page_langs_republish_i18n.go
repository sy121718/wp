package migrations

import "sync"

func registerPageLangsRepublishI18n() {
	registerPageLangsRepublishI18nOnce.Do(registerPageLangsRepublishI18nSeed)
}

var registerPageLangsRepublishI18nOnce sync.Once

func registerPageLangsRepublishI18nSeed() {

	registerSeed(Seed{
		Version:   "494-i18n-page-langs-republish",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.page.langs.action.republish','admin.page.langs.republished'" +
			")",
		SQL: mustSQL("494_page_langs_republish_i18n.sql"),
	})
}

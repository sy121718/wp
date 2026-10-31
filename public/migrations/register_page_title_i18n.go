package migrations

import "sync"

func registerPageTitleI18n() {
	registerPageTitleI18nOnce.Do(registerPageTitleI18nSeed)
}

var registerPageTitleI18nOnce sync.Once

func registerPageTitleI18nSeed() {
	registerSeed(Seed{
		Version:   "520-page-title-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN (SELECT COUNT(*) FROM sys_i18n " +
			"WHERE item_key IN ('admin.system.title') " +
			"AND lang IN ('zh-CN', 'en-US')) >= 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("520_page_title_i18n.sql"),
	})
}

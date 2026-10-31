package migrations

import "sync"

func registerNavigationWriteUII18n() {
	registerNavigationWriteUII18nOnce.Do(registerNavigationWriteUII18nSeed)
}

var registerNavigationWriteUII18nOnce sync.Once

func registerNavigationWriteUII18nSeed() {

	registerSeed(Seed{
		Version:   "600-navigation-write-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.navigations.write.noJs', " +
			"'admin.navigations.write.refresh')",
		SQL: mustSQL("600_navigation_write_ui_i18n.sql"),
	})
}

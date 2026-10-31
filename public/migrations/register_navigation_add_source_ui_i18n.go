package migrations

import "sync"

func registerNavigationAddSourceUII18n() {
	registerNavigationAddSourceUII18nOnce.Do(registerNavigationAddSourceUII18nSeed)
}

var registerNavigationAddSourceUII18nOnce sync.Once

func registerNavigationAddSourceUII18nSeed() {

	registerSeed(Seed{
		Version:   "601-navigation-add-source-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 6 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.navigations.addSource.result.allAdded', " +
			"'admin.navigations.addSource.result.allSkipped', " +
			"'admin.navigations.addSource.result.partial')",
		SQL: mustSQL("601_navigation_add_source_ui_i18n.sql"),
	})
}

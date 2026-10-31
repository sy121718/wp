package migrations

import "sync"

func registerNavigationSortTiedI18n() {
	registerNavigationSortTiedI18nOnce.Do(registerNavigationSortTiedI18nSeed)
}

var registerNavigationSortTiedI18nOnce sync.Once

func registerNavigationSortTiedI18nSeed() {

	registerSeed(Seed{
		Version:   "602-navigation-sort-tied-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN ('ErrSortTied')",
		SQL: mustSQL("602_navigation_sort_tied_i18n.sql"),
	})
}

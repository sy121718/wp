package migrations

import "sync"

func registerNavigationJumpI18n() {
	registerNavigationJumpI18nOnce.Do(registerNavigationJumpI18nSeed)
}

var registerNavigationJumpI18nOnce sync.Once

func registerNavigationJumpI18nSeed() {

	registerSeed(Seed{
		Version:   "591-navigation-jump-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN ('admin.navigations.actionDone')",
		SQL: mustSQL("591_navigation_jump_i18n.sql"),
	})
}

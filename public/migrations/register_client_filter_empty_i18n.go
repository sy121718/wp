package migrations

import "sync"

func registerClientFilterEmptyI18n() {
	registerClientFilterEmptyI18nOnce.Do(registerClientFilterEmptyI18nSeed)
}

var registerClientFilterEmptyI18nOnce sync.Once

func registerClientFilterEmptyI18nSeed() {

	registerSeed(Seed{
		Version:   "400-i18n-client-filter-empty",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' " +
			"AND item_key IN ('admin.menus.filter_empty','admin.depts.filter_empty')",
		SQL: mustSQL("400_i18n_client_filter_empty.sql"),
	})
}

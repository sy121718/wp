package migrations

import "sync"

func registerDateFilterI18n() {
	registerDateFilterI18nOnce.Do(registerDateFilterI18nSeed)
}

var registerDateFilterI18nOnce sync.Once

func registerDateFilterI18nSeed() {
	registerSeed(Seed{
		Version:   "584-date-filter-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("584_date_filter_i18n.sql"),

		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key = 'admin.common.filter.preset'",
	})
}

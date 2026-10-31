package migrations

import "sync"

func registerRemoveOrphanFilterI18n() {
	registerRemoveOrphanFilterI18nOnce.Do(registerRemoveOrphanFilterI18nSeed)
}

var registerRemoveOrphanFilterI18nOnce sync.Once

func registerRemoveOrphanFilterI18nSeed() {
	registerSeed(Seed{
		Version:   "585-remove-orphan-filter-i18n",
		TableName: "sys_i18n",
		SQL:       mustSQL("585_remove_orphan_filter_i18n.sql"),
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.analytics.range.from', 'admin.analytics.range.to', " +
			"'admin.analytics.range.submit', 'admin.analytics.range.last30', " +
			"'admin.masterdata.action.filter')",
	})
}
